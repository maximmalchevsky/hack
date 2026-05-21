// Package notifier — фабрика уведомлений с приоритетами.
//
// Базовый путь — rule-based: берёт HR-Roadmap-items с priority >= high
// и пушит каждому получателю (сам сотрудник + руководитель + HR).
//
// AI-усиление (опционально, если передан ai.Client): на этапе сборки
// текста уведомления просит GigaChat сгенерировать короткий персональный
// title + body. На любую ошибку — fall-back на rule-based.
package notifier

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"worktimesync/internal/ai"
	"worktimesync/internal/ai/prompts"
	"worktimesync/internal/service"
)

type SmartNotifier struct {
	pool          *pgxpool.Pool
	hrRoadmap     *service.HRRoadmapService
	notifications *service.NotificationService
	llm           ai.Client
	log           zerolog.Logger

	sentDedupTTL time.Duration
}

func NewSmartNotifier(
	pool *pgxpool.Pool,
	roadmap *service.HRRoadmapService,
	notifications *service.NotificationService,
	llm ai.Client,
	log zerolog.Logger,
) *SmartNotifier {
	return &SmartNotifier{
		pool:          pool,
		hrRoadmap:     roadmap,
		notifications: notifications,
		llm:           llm,
		log:           log,
		sentDedupTTL:  24 * time.Hour,
	}
}

// Run — один проход. Возвращает количество отправленных уведомлений.
//
// Использование:
//   - Asynq scheduler запускает задачу `notifications:send` раз в час.
//   - Handler Asynq вызывает Run и логирует результат.
func (n *SmartNotifier) Run(ctx context.Context) (int, error) {
	items, err := n.hrRoadmap.Build(ctx, 50)
	if err != nil {
		return 0, fmt.Errorf("smart-notifier: build roadmap: %w", err)
	}

	sent := 0
	for _, it := range items {
		if it.Priority != "critical" && it.Priority != "high" {
			continue
		}

		// Определяем получателя: HR + руководитель + сам сотрудник.
		recipients, err := n.recipientsFor(ctx, it.EmployeeID)
		if err != nil {
			n.log.Warn().Err(err).Str("employee", it.FullName).Msg("smart-notifier: recipients lookup failed")
			continue
		}

		// Один AI-вызов на сотрудника — текст одинаков для всех получателей.
		title, body := n.composeText(ctx, it)

		for _, userID := range recipients {
			// Дедуп: не слать повторно тот же kind+subject за последние 24ч.
			alreadySent, err := n.alreadySentRecently(ctx, userID, "stale_profile", it.EmployeeID)
			if err != nil {
				continue
			}
			if alreadySent {
				continue
			}

			_, err = n.notifications.Push(ctx, service.CreateInput{
				UserID: userID,
				Kind:   "stale_profile",
				Title:  title,
				Body:   body,
				Link:   "/employees/" + it.EmployeeID.String(),
				Payload: map[string]any{
					"subject_id":    it.EmployeeID.String(),
					"priority":      it.Priority,
					"action":        it.Action,
					"days_since":    it.DaysSinceUpdate,
				},
			})
			if err != nil {
				n.log.Warn().Err(err).Msg("smart-notifier: push failed")
				continue
			}
			sent++
		}
	}
	return sent, nil
}

// recipientsFor — кому слать уведомление о сотруднике с устаревшим графиком.
// Стратегия: сам сотрудник + его руководитель + все HR.
func (n *SmartNotifier) recipientsFor(ctx context.Context, employeeID uuid.UUID) ([]uuid.UUID, error) {
	var out []uuid.UUID

	// 1. Сам сотрудник.
	var selfUserID uuid.UUID
	if err := n.pool.QueryRow(ctx, `
		SELECT user_id FROM employees WHERE id = $1
	`, employeeID).Scan(&selfUserID); err == nil {
		out = append(out, selfUserID)
	}

	// 2. Его руководитель (если задан).
	var managerEmployeeID *uuid.UUID
	_ = n.pool.QueryRow(ctx, `
		SELECT manager_id FROM employees WHERE id = $1
	`, employeeID).Scan(&managerEmployeeID)
	if managerEmployeeID != nil {
		var managerUserID uuid.UUID
		if err := n.pool.QueryRow(ctx, `
			SELECT user_id FROM employees WHERE id = $1
		`, *managerEmployeeID).Scan(&managerUserID); err == nil {
			out = append(out, managerUserID)
		}
	}

	// 3. Все HR.
	rows, err := n.pool.Query(ctx, `
		SELECT id FROM users WHERE role = 'hr'
	`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var hrID uuid.UUID
			if err := rows.Scan(&hrID); err == nil {
				out = append(out, hrID)
			}
		}
	}
	return uniqueUUIDs(out), nil
}

func (n *SmartNotifier) alreadySentRecently(ctx context.Context, userID uuid.UUID, kind string, subjectID uuid.UUID) (bool, error) {
	var count int
	err := n.pool.QueryRow(ctx, `
		SELECT COUNT(*)
		FROM notifications
		WHERE user_id = $1
		  AND kind = $2
		  AND payload->>'subject_id' = $3
		  AND created_at > $4
	`, userID, kind, subjectID.String(), time.Now().Add(-n.sentDedupTTL)).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

// composeText — заголовок и тело уведомления. Если есть LLM, просит модель
// написать короткий персональный текст (~140 символов в body), иначе — rule-based.
func (n *SmartNotifier) composeText(ctx context.Context, it service.HRRoadmapItem) (string, string) {
	if n.llm != nil {
		if t, b, err := n.aiCompose(ctx, it); err == nil && t != "" && b != "" {
			return t, b
		} else if err != nil {
			n.log.Debug().Err(err).Msg("smart-notifier: ai compose failed, fallback to rules")
		}
	}
	return buildTitle(it.FullName, it.DaysSinceUpdate), buildBody(it.Reason, it.Action)
}

func (n *SmartNotifier) aiCompose(ctx context.Context, it service.HRRoadmapItem) (string, string, error) {
	payload := map[string]any{
		"subject_name":      it.FullName,
		"days_since_update": it.DaysSinceUpdate,
		"reason":            it.Reason,
		"action":            it.Action,
		"priority":          it.Priority,
	}
	raw, err := json.Marshal(payload)
	if err != nil {
		return "", "", err
	}
	resp, err := n.llm.Complete(ctx, ai.CompletionRequest{
		Messages: []ai.Message{
			{Role: ai.RoleSystem, Content: prompts.SmartNotifier},
			{Role: ai.RoleUser, Content: string(raw)},
		},
		Temperature: 0.2,
		MaxTokens:   220,
		JSONMode:    true,
	})
	if err != nil {
		return "", "", err
	}
	if resp == nil || resp.Content == "" {
		return "", "", fmt.Errorf("empty response")
	}
	clean := stripFence(resp.Content)
	// Промпт просит JSON с массивом, но для одного кандидата можем получить и
	// просто {"title": "...", "body": "..."} — обрабатываем оба варианта.
	var single struct {
		Title string `json:"title"`
		Body  string `json:"body"`
	}
	if err := json.Unmarshal([]byte(clean), &single); err == nil && single.Title != "" {
		return strings.TrimSpace(single.Title), strings.TrimSpace(single.Body), nil
	}
	var wrapped struct {
		ToSend []struct {
			Title string `json:"title"`
			Body  string `json:"body"`
		} `json:"to_send"`
	}
	if err := json.Unmarshal([]byte(clean), &wrapped); err == nil && len(wrapped.ToSend) > 0 {
		t := wrapped.ToSend[0]
		return strings.TrimSpace(t.Title), strings.TrimSpace(t.Body), nil
	}
	return "", "", fmt.Errorf("unrecognized json shape")
}

func stripFence(s string) string {
	s = strings.TrimSpace(s)
	if strings.HasPrefix(s, "```") {
		if i := strings.IndexByte(s, '\n'); i >= 0 {
			s = s[i+1:]
		} else {
			s = strings.TrimPrefix(s, "```json")
			s = strings.TrimPrefix(s, "```")
		}
		s = strings.TrimSuffix(s, "```")
	}
	return strings.TrimSpace(s)
}

func buildTitle(name string, days int) string {
	switch {
	case days > 90:
		return fmt.Sprintf("%s давно не обновлял график (%d дн.)", name, days)
	case days > 60:
		return fmt.Sprintf("%s — пора обновить рабочий профиль", name)
	default:
		return fmt.Sprintf("%s — стоит подтвердить актуальность", name)
	}
}

func buildBody(reason, action string) string {
	a := strings.ReplaceAll(action, "_", " ")
	return reason + ". Действие: " + a
}

func uniqueUUIDs(in []uuid.UUID) []uuid.UUID {
	seen := make(map[uuid.UUID]struct{}, len(in))
	out := make([]uuid.UUID, 0, len(in))
	for _, id := range in {
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

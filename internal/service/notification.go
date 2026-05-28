package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog/log"

	"worktimesync/internal/domain"
	"worktimesync/internal/notify"
	"worktimesync/internal/repository"
)

const (
	WSNotificationsChannel = "ws:user:"
)

type NotificationService struct {
	pool       *pgxpool.Pool
	repo       *repository.NotificationRepo
	redis      *redis.Client
	transports []notify.Transport
}

func NewNotificationService(pool *pgxpool.Pool, rdb *redis.Client) *NotificationService {
	return &NotificationService{
		pool:  pool,
		repo:  repository.NewNotificationRepo(pool),
		redis: rdb,
	}
}

func (s *NotificationService) WithTransports(ts ...notify.Transport) *NotificationService {
	for _, t := range ts {
		if t == nil {
			continue
		}
		if ec, ok := t.(notify.EnabledChecker); ok && !ec.Enabled() {
			continue
		}
		s.transports = append(s.transports, t)
	}
	return s
}

type CreateInput struct {
	UserID  uuid.UUID
	Kind    string
	Title   string
	Body    string
	Link    string
	Payload map[string]any
}

func (s *NotificationService) Push(ctx context.Context, in CreateInput) (*domain.Notification, error) {
	var raw []byte
	if in.Payload != nil {
		raw, _ = json.Marshal(in.Payload)
	}
	n, err := s.repo.Create(ctx, repository.CreateNotificationInput{
		UserID:  in.UserID,
		Kind:    in.Kind,
		Title:   in.Title,
		Body:    in.Body,
		Link:    in.Link,
		Payload: raw,
	})
	if err != nil {
		return nil, err
	}

	if s.redis != nil {
		msg, _ := json.Marshal(map[string]any{
			"type":         "notification.created",
			"notification": notificationToMap(*n),
		})
		_ = s.redis.Publish(ctx, WSNotificationsChannel+in.UserID.String(), msg).Err()
	}

	if len(s.transports) > 0 {
		go s.dispatchToTransports(in.UserID, *n)
	}

	return n, nil
}

func (s *NotificationService) dispatchToTransports(userID uuid.UUID, n domain.Notification) {
	ctx, cancel := context.WithTimeout(context.Background(), 15_000_000_000)
	defer cancel()

	var (
		email, name string
		emailOn     bool
		tgChat      *string
		tgOn        bool
		kinds       []string
		minPriority string
	)
	if err := s.pool.QueryRow(ctx, `
		SELECT u.email, u.full_name,
		       u.email_notifications, u.telegram_chat_id, u.telegram_notifications,
		       u.notify_kinds, u.notify_min_priority
		FROM users u WHERE u.id = $1
	`, userID).Scan(&email, &name, &emailOn, &tgChat, &tgOn, &kinds, &minPriority); err != nil {
		log.Warn().Err(err).Str("user_id", userID.String()).Msg("notify: load prefs failed")
		return
	}

	if len(kinds) > 0 {
		allowed := false
		for _, k := range kinds {
			if k == n.Kind {
				allowed = true
				break
			}
		}
		if !allowed {
			return
		}
	}

	pr := priorityOfKind(n.Kind)
	if priorityRank(pr) < priorityRank(minPriority) {
		return
	}

	tg := ""
	if tgChat != nil {
		tg = *tgChat
	}

	msg := notify.Message{
		UserID:     userID.String(),
		UserName:   name,
		UserEmail:  email,
		TelegramID: tg,
		Title:      n.Title,
		Body:       n.Body,
		Link:       n.Link,
		Kind:       n.Kind,
	}

	for _, t := range s.transports {
		switch t.Name() {
		case "email":
			if !emailOn || email == "" {
				continue
			}
		case "telegram":
			if !tgOn || tg == "" {
				continue
			}
		}
		if err := t.Send(ctx, msg); err != nil {
			log.Warn().Err(err).Str("transport", t.Name()).Str("kind", n.Kind).Msg("notify: send failed")
		}
	}
}

func (s *NotificationService) List(ctx context.Context, userID uuid.UUID, onlyUnread bool) ([]domain.Notification, error) {
	return s.repo.ListByUser(ctx, userID, onlyUnread, 50)
}

func (s *NotificationService) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	return s.repo.CountUnread(ctx, userID)
}

func (s *NotificationService) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	err := s.repo.MarkRead(ctx, id, userID)
	if errors.Is(err, repository.ErrNotFound) {
		return errors.New("notification: not found or already read")
	}
	return err
}

func (s *NotificationService) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	return s.repo.MarkAllRead(ctx, userID)
}

func priorityOfKind(kind string) string {
	switch kind {
	case "meeting_proposal", "meeting_reminder", "meeting_response",
		"meeting_updated", "meeting_cancelled", "event_reminder":
		return "high"
	case "recommendation", "pulse_check_due", "system":
		return "medium"
	case "team_digest", "weekly_summary", "stale_profile":
		return "low"
	default:
		return "medium"
	}
}

func priorityRank(p string) int {
	switch p {
	case "high":
		return 2
	case "medium":
		return 1
	default:
		return 0
	}
}

type NotifyBatchResult struct {
	Sent     int      `json:"sent"`
	Skipped  int      `json:"skipped"`
	Targeted int      `json:"targeted"`
	Emails   []string `json:"emails,omitempty"`
}

var notifyTemplates = map[string]struct {
	title string
	body  string
	link  string
}{
	"burnout": {
		title: "Высокая нагрузка — посмотри график",
		body:  "Система отметила тебя в зоне риска выгорания. Зайди в /workload и проверь нагрузку — возможно стоит перенести часть встреч.",
		link:  "/workload",
	},
	"overload": {
		title: "Перегруз по задачам",
		body:  "В плане задач не хватает времени до дедлайнов. Зайди в /tasks и пересмотри оценки или сроки.",
		link:  "/tasks",
	},
	"anomaly": {
		title: "Необычная активность",
		body:  "В последние дни активность сильно отличается от обычного ритма. Проверь свой график в /profile — возможно его пора обновить.",
		link:  "/profile",
	},
	"stale_profile": {
		title: "Пожалуйста, обнови рабочий график",
		body:  "Профиль давно не обновлялся. Зайди в /profile и подтверди актуальные рабочие часы.",
		link:  "/profile",
	},
}

func (s *NotificationService) NotifyByKind(
	ctx context.Context,
	kind string,
	empIDs []uuid.UUID,
	initiatorUser uuid.UUID,
) (*NotifyBatchResult, error) {
	tpl, ok := notifyTemplates[kind]
	if !ok {
		return nil, fmt.Errorf("notify: unknown kind %q", kind)
	}
	if len(empIDs) == 0 {
		return &NotifyBatchResult{}, nil
	}

	rows, err := s.pool.Query(ctx, `
		SELECT u.id, COALESCE(u.email, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, empIDs)
	if err != nil {
		return nil, err
	}
	type rec struct {
		userID uuid.UUID
		email  string
	}
	var cands []rec
	for rows.Next() {
		var r rec
		if err := rows.Scan(&r.userID, &r.email); err != nil {
			continue
		}
		cands = append(cands, r)
	}
	rows.Close()

	recent := map[uuid.UUID]struct{}{}
	if len(cands) > 0 {
		ids := make([]uuid.UUID, 0, len(cands))
		for _, c := range cands {
			ids = append(ids, c.userID)
		}
		dr, derr := s.pool.Query(ctx, `
			SELECT user_id FROM notifications
			WHERE kind = $1
			  AND user_id = ANY($2)
			  AND created_at > now() - interval '24 hours'
		`, kind, ids)
		if derr == nil {
			for dr.Next() {
				var u uuid.UUID
				if scanErr := dr.Scan(&u); scanErr == nil {
					recent[u] = struct{}{}
				}
			}
			dr.Close()
		}
	}

	res := &NotifyBatchResult{Targeted: len(cands)}
	for _, c := range cands {
		if _, dup := recent[c.userID]; dup {
			res.Skipped++
			continue
		}
		_, err := s.Push(ctx, CreateInput{
			UserID: c.userID,
			Kind:   kind,
			Title:  tpl.title,
			Body:   tpl.body,
			Link:   tpl.link,
			Payload: map[string]any{
				"initiator_id": initiatorUser.String(),
			},
		})
		if err != nil {
			continue
		}
		res.Sent++
		if c.email != "" {
			res.Emails = append(res.Emails, c.email)
		}
	}
	return res, nil
}

func notificationToMap(n domain.Notification) map[string]any {
	m := map[string]any{
		"id":         n.ID,
		"kind":       n.Kind,
		"title":      n.Title,
		"created_at": n.CreatedAt,
	}
	if n.Body != "" {
		m["body"] = n.Body
	}
	if n.Link != "" {
		m["link"] = n.Link
	}
	return m
}

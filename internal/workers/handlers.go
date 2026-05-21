package workers

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"worktimesync/internal/service"
	"worktimesync/pkg/locks"
)

// Deps — зависимости handler'ов. Поля могут быть nil, если соответствующая
// функциональность ещё не подключена (например, в раннем спринте).
type Deps struct {
	Log             zerolog.Logger
	Pool            *pgxpool.Pool
	Locks           *locks.Manager
	Sync            *service.SyncService
	Recommendations *service.RecommendationService
	Enqueuer        *Enqueuer
	Notifier        SmartNotifierRunner
	Notifications   *service.NotificationService // для reminder-сканера
}

// SmartNotifierRunner — лёгкий интерфейс, чтобы workers/ не зависел от notifier-пакета.
type SmartNotifierRunner interface {
	Run(ctx context.Context) (int, error)
}

// Handlers — обработчики Asynq-задач.
type Handlers struct {
	deps Deps
}

func NewHandlers(deps Deps) *Handlers { return &Handlers{deps: deps} }

// Register регистрирует все обработчики в mux.
func (h *Handlers) Register(mux *asynq.ServeMux) {
	mux.HandleFunc(TaskSyncIncremental, h.handleSync)
	mux.HandleFunc(TaskSyncBackfill, h.handleSync) // одна реализация на оба
	mux.HandleFunc(TaskOAuthRefresh, h.handleStub)
	mux.HandleFunc(TaskOAuthRefreshGigaChat, h.handleStub)
	mux.HandleFunc(TaskMetricsRecompute, h.handleMetricsRecompute)
	mux.HandleFunc(TaskTeamAvailabilityRebuild, h.handleStub)
	mux.HandleFunc(TaskAIRecommend, h.handleAIRecommend)
	mux.HandleFunc(TaskNotificationSend, h.handleNotificationSend)
	mux.HandleFunc(TaskDigestDaily, h.handleDigestDaily)
	mux.HandleFunc(TaskReminderScan, h.handleReminderScan)
}

// handleReminderScan — раз в минуту смотрит, какие события стартуют в окне
// [now+14m, now+16m], и пушит уведомление-напоминание. Дедуп — через
// проверку, что для этого event_id за последние 30 минут уже не было
// reminder-нотификации.
//
// Логика идемпотентна: даже если задача сработает дважды подряд, второй
// раз дедуп пропустит всех.
func (h *Handlers) handleReminderScan(ctx context.Context, t *asynq.Task) error {
	if h.deps.Notifications == nil || h.deps.Pool == nil {
		return nil
	}

	rows, err := h.deps.Pool.Query(ctx, `
		SELECT ce.id, ce.title, ce.start_at, ce.end_at, e.user_id
		FROM calendar_events ce
		JOIN employees e ON e.id = ce.employee_id
		WHERE ce.is_excluded = false
		  AND ce.status <> 'cancelled'
		  AND ce.start_at >= now() + interval '14 minutes'
		  AND ce.start_at <= now() + interval '16 minutes'
		  AND NOT EXISTS (
			SELECT 1 FROM notifications n
			WHERE n.user_id = e.user_id
			  AND n.kind = 'event_reminder'
			  AND COALESCE(n.payload->>'event_id', '') = ce.id::text
			  AND n.created_at > now() - interval '30 minutes'
		  )
	`)
	if err != nil {
		return fmt.Errorf("reminder scan query: %w", err)
	}
	defer rows.Close()

	type ev struct {
		EventID uuid.UUID
		Title   string
		StartAt time.Time
		EndAt   time.Time
		UserID  uuid.UUID
	}
	var todo []ev
	for rows.Next() {
		var e ev
		if err := rows.Scan(&e.EventID, &e.Title, &e.StartAt, &e.EndAt, &e.UserID); err != nil {
			continue
		}
		todo = append(todo, e)
	}
	if err := rows.Err(); err != nil {
		return err
	}

	pushed := 0
	for _, e := range todo {
		title := e.Title
		if title == "" {
			title = "Встреча"
		}
		body := fmt.Sprintf("Через 15 минут — %s до %s.",
			e.StartAt.Format("15:04"),
			e.EndAt.Format("15:04"),
		)
		if _, perr := h.deps.Notifications.Push(ctx, service.CreateInput{
			UserID: e.UserID,
			Kind:   "event_reminder",
			Title:  title,
			Body:   body,
			Link:   "/dashboard",
			Payload: map[string]any{
				"event_id":  e.EventID.String(),
				"start_at":  e.StartAt,
				"end_at":    e.EndAt,
			},
		}); perr == nil {
			pushed++
		}
	}
	if pushed > 0 {
		h.deps.Log.Info().Int("pushed", pushed).Int("scanned", len(todo)).Msg("event reminders sent")
	}
	return nil
}

// handleMetricsRecompute — пересчитывает A/C/L/Z/H/R одного сотрудника,
// кладёт в Redis-кэш (через RecommendationService.ComputeMetrics).
//
// Использование distributed lock: одна и та же задача может прилететь
// несколько раз (sync завершился, профиль изменился) — лочим на 30 сек.
func (h *Handlers) handleMetricsRecompute(ctx context.Context, t *asynq.Task) error {
	if h.deps.Recommendations == nil {
		h.deps.Log.Warn().Str("type", t.Type()).Msg("recommendations service not configured, skipping")
		return nil
	}

	var p MetricsRecomputePayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if p.EmployeeID == uuid.Nil {
		return fmt.Errorf("empty employee_id")
	}

	run := func(ctx context.Context) error {
		// Инвалидируем кэш и пересчитываем заново.
		h.deps.Recommendations.InvalidateMetrics(ctx, p.EmployeeID)
		m, err := h.deps.Recommendations.ComputeMetrics(ctx, p.EmployeeID)
		if err != nil {
			return err
		}
		h.deps.Log.Info().
			Str("employee_id", p.EmployeeID.String()).
			Float64("A", m.A).Float64("C", m.C).Float64("L", m.L).
			Float64("Z", m.Z).Float64("H", m.H).Float64("R", m.R).
			Msg("metrics recomputed")
		return nil
	}

	if h.deps.Locks != nil {
		executed, err := h.deps.Locks.TryLockOrSkip(ctx,
			"metrics:recompute:"+p.EmployeeID.String(),
			30*time.Second, run)
		if err != nil {
			return err
		}
		if !executed {
			h.deps.Log.Debug().
				Str("employee_id", p.EmployeeID.String()).
				Msg("metrics recompute skipped: lock held")
		}
		return nil
	}
	return run(ctx)
}

// handleAIRecommend — перегенерация рекомендаций для одного сотрудника.
// Зовёт RecommendationService.Generate (тот же путь, что POST /recommendations/generate).
func (h *Handlers) handleAIRecommend(ctx context.Context, t *asynq.Task) error {
	if h.deps.Recommendations == nil {
		h.deps.Log.Warn().Str("type", t.Type()).Msg("recommendations service not configured, skipping")
		return nil
	}

	var p AIRecommendPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}
	if p.EmployeeID == uuid.Nil {
		return fmt.Errorf("empty employee_id")
	}

	run := func(ctx context.Context) error {
		recs, err := h.deps.Recommendations.Generate(ctx, p.EmployeeID)
		if err != nil {
			return err
		}
		h.deps.Log.Info().
			Str("employee_id", p.EmployeeID.String()).
			Int("count", len(recs)).
			Msg("ai recommendations regenerated")
		return nil
	}

	if h.deps.Locks != nil {
		executed, err := h.deps.Locks.TryLockOrSkip(ctx,
			"ai:recommend:"+p.EmployeeID.String(),
			2*time.Minute, run)
		if err != nil {
			return err
		}
		if !executed {
			h.deps.Log.Debug().
				Str("employee_id", p.EmployeeID.String()).
				Msg("ai recommend skipped: lock held")
		}
		return nil
	}
	return run(ctx)
}

// handleDigestDaily — ночной batch: проходит по всем сотрудникам и
// ставит каждому ai:recommend в очередь. Так весь компанию обновляем за раз,
// но без локов на огромных транзакциях.
func (h *Handlers) handleDigestDaily(ctx context.Context, t *asynq.Task) error {
	if h.deps.Pool == nil || h.deps.Enqueuer == nil {
		h.deps.Log.Warn().Msg("digest:daily skipped: deps not configured")
		return nil
	}
	rows, err := h.deps.Pool.Query(ctx, `SELECT id FROM employees ORDER BY id`)
	if err != nil {
		return fmt.Errorf("list employees: %w", err)
	}
	defer rows.Close()

	enq := 0
	for rows.Next() {
		var empID uuid.UUID
		if err := rows.Scan(&empID); err != nil {
			continue
		}
		if err := h.deps.Enqueuer.EnqueueAIRecommend(empID); err != nil {
			h.deps.Log.Warn().Err(err).
				Str("employee_id", empID.String()).
				Msg("digest: enqueue failed")
			continue
		}
		enq++
	}
	h.deps.Log.Info().Int("enqueued", enq).Msg("digest:daily fan-out complete")
	return rows.Err()
}

// handleNotificationSend — запускает smart-notifier для отправки уведомлений
// о сотрудниках с устаревшими графиками. Дёргается scheduler'ом ежечасно.
func (h *Handlers) handleNotificationSend(ctx context.Context, t *asynq.Task) error {
	if h.deps.Notifier == nil {
		h.deps.Log.Warn().Str("type", t.Type()).Msg("notifier not configured, skipping")
		return nil
	}
	sent, err := h.deps.Notifier.Run(ctx)
	if err != nil {
		h.deps.Log.Error().Err(err).Msg("smart-notifier failed")
		return err
	}
	h.deps.Log.Info().Int("sent", sent).Msg("smart-notifier done")
	return nil
}

func (h *Handlers) handleSync(ctx context.Context, t *asynq.Task) error {
	if h.deps.Sync == nil {
		h.deps.Log.Warn().Str("type", t.Type()).Msg("sync skipped: no Sync service in worker deps")
		return nil
	}

	var p SyncPayload
	if err := json.Unmarshal(t.Payload(), &p); err != nil {
		return fmt.Errorf("decode payload: %w", err)
	}

	// distributed lock — чтобы одна интеграция не синхронизировалась дважды одновременно.
	if h.deps.Locks != nil {
		executed, err := h.deps.Locks.TryLockOrSkip(ctx,
			"sync:integration:"+p.IntegrationID.String(),
			3*time.Minute,
			func(ctx context.Context) error {
				return h.runSync(ctx, p.IntegrationID.String(), t.Type())
			})
		if err != nil {
			return err
		}
		if !executed {
			h.deps.Log.Info().
				Str("integration_id", p.IntegrationID.String()).
				Msg("sync skipped: lock held by another worker")
		}
		return nil
	}
	return h.runSync(ctx, p.IntegrationID.String(), t.Type())
}

func (h *Handlers) runSync(ctx context.Context, idStr, taskType string) error {
	uid, err := uuid.Parse(idStr)
	if err != nil {
		return fmt.Errorf("parse integration_id %q: %w", idStr, err)
	}
	res, err := h.deps.Sync.SyncIntegration(ctx, uid)
	if err != nil {
		h.deps.Log.Error().
			Err(err).
			Str("integration_id", idStr).
			Str("task", taskType).
			Msg("sync failed")
		return err
	}
	h.deps.Log.Info().
		Str("integration_id", idStr).
		Str("provider", string(res.Provider)).
		Int("events_loaded", res.EventsLoaded).
		Msg("sync done")

	// После успешного sync пересчитываем метрики и обновляем рекомендации
	// для сотрудника, чьи события только что приехали.
	if h.deps.Enqueuer != nil && res.EmployeeID != uuid.Nil {
		_ = h.deps.Enqueuer.EnqueueMetricsRecompute(res.EmployeeID)
		_ = h.deps.Enqueuer.EnqueueAIRecommend(res.EmployeeID)
	}
	return nil
}

func (h *Handlers) handleStub(ctx context.Context, t *asynq.Task) error {
	h.deps.Log.Info().Str("type", t.Type()).Msg("task received (stub)")
	return nil
}

package repository

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

// TrackerTaskRepo — задачи из Jira/Yandex Tracker + связанные планы.
type TrackerTaskRepo struct {
	pool *pgxpool.Pool
}

func NewTrackerTaskRepo(pool *pgxpool.Pool) *TrackerTaskRepo {
	return &TrackerTaskRepo{pool: pool}
}

// UpsertTaskInput — параметры Upsert. Каноничный путь записи задачи из sync'а.
type UpsertTaskInput struct {
	EmployeeID     uuid.UUID
	IntegrationID  *uuid.UUID
	SourceTaskID   string
	Title          string
	Description    string
	Status         string
	Priority       domain.TaskPriority
	Type           string
	DueAt          *time.Time
	EstimatedHours *float64
	ActualHours    *float64
	Raw            map[string]any
}

// Upsert — INSERT…ON CONFLICT по (integration_id, source_task_id).
// При повторном вызове обновляет описательные поля, но НЕ затирает
// ai_estimated_hours/ai_estimate_confidence (их пишет TaskEstimator отдельно).
// estimated_hours тоже не затирается, если новое значение nil — пользователь
// мог поставить ручную оценку, и следующий sync без estimate её не должен сбить.
func (r *TrackerTaskRepo) Upsert(ctx context.Context, in UpsertTaskInput) (*domain.TrackerTask, error) {
	rawJSON, _ := json.Marshal(in.Raw)
	row := r.pool.QueryRow(ctx, `
		INSERT INTO tracker_tasks
			(employee_id, integration_id, source_task_id, title, description,
			 status, priority, task_type, due_at, estimated_hours, actual_hours,
			 raw, fetched_at)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''),
		        NULLIF($6, ''), NULLIF($7, ''), NULLIF($8, ''), $9, $10, $11,
		        $12, now())
		ON CONFLICT (integration_id, source_task_id) WHERE integration_id IS NOT NULL
		DO UPDATE SET
			title           = EXCLUDED.title,
			description     = EXCLUDED.description,
			status          = EXCLUDED.status,
			priority        = EXCLUDED.priority,
			task_type       = EXCLUDED.task_type,
			due_at          = EXCLUDED.due_at,
			-- сохраняем ручной estimate если в новом sync пришло пусто:
			estimated_hours = COALESCE(EXCLUDED.estimated_hours, tracker_tasks.estimated_hours),
			actual_hours    = COALESCE(EXCLUDED.actual_hours, tracker_tasks.actual_hours),
			raw             = EXCLUDED.raw,
			fetched_at      = now()
		RETURNING id, employee_id, integration_id, source_task_id,
		          COALESCE(title, ''), COALESCE(description, ''),
		          COALESCE(status, ''), COALESCE(priority, ''),
		          COALESCE(task_type, ''), due_at,
		          estimated_hours, actual_hours,
		          ai_estimated_hours, ai_estimate_confidence,
		          fetched_at
	`,
		in.EmployeeID, in.IntegrationID, in.SourceTaskID,
		in.Title, in.Description,
		in.Status, string(in.Priority), in.Type,
		in.DueAt, in.EstimatedHours, in.ActualHours,
		rawJSON,
	)
	return scanTrackerTask(row)
}

// SetAIEstimate — заполняет ai_estimated_hours / ai_estimate_confidence
// после успешного дёргания GigaChat'а. Перезаписывает прошлую AI-оценку.
func (r *TrackerTaskRepo) SetAIEstimate(ctx context.Context, taskID uuid.UUID, hours, confidence float64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tracker_tasks
		SET ai_estimated_hours = $1, ai_estimate_confidence = $2
		WHERE id = $3
	`, hours, confidence, taskID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetManualEstimate — пользователь принял оценку AI или вписал свою цифру.
// Идёт в estimated_hours (важно для planner.EffectiveEstimate).
func (r *TrackerTaskRepo) SetManualEstimate(ctx context.Context, taskID, empID uuid.UUID, hours float64) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE tracker_tasks
		SET estimated_hours = $1
		WHERE id = $2 AND employee_id = $3
	`, hours, taskID, empID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// ListTasksFilter — фильтры для ListByEmployee.
type ListTasksFilter struct {
	EmployeeID         uuid.UUID
	IncludeDone        bool      // по умолчанию false — Done скрываем
	OnlyDueBefore      time.Time // если задано — задачи с due_at <= этой даты или без due_at
	OnlyMissingEstimate bool     // только задачи где estimated_hours IS NULL
}

func (r *TrackerTaskRepo) ListByEmployee(ctx context.Context, f ListTasksFilter) ([]domain.TrackerTask, error) {
	sql := `
		SELECT id, employee_id, integration_id, source_task_id,
		       COALESCE(title, ''), COALESCE(description, ''),
		       COALESCE(status, ''), COALESCE(priority, ''),
		       COALESCE(task_type, ''), due_at,
		       estimated_hours, actual_hours,
		       ai_estimated_hours, ai_estimate_confidence,
		       fetched_at
		FROM tracker_tasks
		WHERE employee_id = $1
	`
	args := []any{f.EmployeeID}
	if !f.IncludeDone {
		sql += " AND (status IS NULL OR lower(status) NOT IN ('done', 'closed', 'resolved'))"
	}
	if !f.OnlyDueBefore.IsZero() {
		args = append(args, f.OnlyDueBefore)
		sql += fmt.Sprintf(" AND (due_at IS NULL OR due_at <= $%d)", len(args))
	}
	if f.OnlyMissingEstimate {
		sql += " AND estimated_hours IS NULL AND ai_estimated_hours IS NULL"
	}
	sql += " ORDER BY due_at NULLS LAST, priority DESC"

	rows, err := r.pool.Query(ctx, sql, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TrackerTask
	for rows.Next() {
		t, err := scanTrackerTask(rows)
		if err != nil {
			continue
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

// SaveSlots — полностью переписывает план задачи: DELETE + INSERT в одной
// транзакции. Слоты с нулевыми/отрицательными часами игнорируются.
//
// Если planner выдал пустой список — это валидно: задача планируется, но
// не помещается в горизонт (deadline_at_risk).
func (r *TrackerTaskRepo) SaveSlots(ctx context.Context, taskID uuid.UUID, slots []domain.TaskPlanSlot) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	if _, err := tx.Exec(ctx, `DELETE FROM task_plan_slots WHERE task_id = $1`, taskID); err != nil {
		return err
	}
	for _, s := range slots {
		if s.Hours <= 0 {
			continue
		}
		if _, err := tx.Exec(ctx, `
			INSERT INTO task_plan_slots (task_id, employee_id, date, hours)
			VALUES ($1, $2, $3, $4)
		`, taskID, s.EmployeeID, s.Date, s.Hours); err != nil {
			return err
		}
	}
	return tx.Commit(ctx)
}

// DeleteAllSlots — DELETE по сотруднику (используется replan'ом перед
// полным пересчётом, чтобы не было «мусорных» слотов от удалённых задач).
func (r *TrackerTaskRepo) DeleteAllSlots(ctx context.Context, empID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `DELETE FROM task_plan_slots WHERE employee_id = $1`, empID)
	return err
}

// ListSlots — слоты сотрудника в окне [from, to]. Для UI (Gantt, dashboard).
func (r *TrackerTaskRepo) ListSlots(ctx context.Context, empID uuid.UUID, from, to time.Time) ([]domain.TaskPlanSlot, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, task_id, employee_id, date, hours, computed_at
		FROM task_plan_slots
		WHERE employee_id = $1 AND date >= $2 AND date <= $3
		ORDER BY date, hours DESC
	`, empID, from, to)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []domain.TaskPlanSlot
	for rows.Next() {
		var s domain.TaskPlanSlot
		if err := rows.Scan(&s.ID, &s.TaskID, &s.EmployeeID, &s.Date, &s.Hours, &s.ComputedAt); err != nil {
			continue
		}
		out = append(out, s)
	}
	return out, rows.Err()
}

// ByID — одна задача по UUID. Используется handler'ом PATCH estimate.
func (r *TrackerTaskRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.TrackerTask, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, employee_id, integration_id, source_task_id,
		       COALESCE(title, ''), COALESCE(description, ''),
		       COALESCE(status, ''), COALESCE(priority, ''),
		       COALESCE(task_type, ''), due_at,
		       estimated_hours, actual_hours,
		       ai_estimated_hours, ai_estimate_confidence,
		       fetched_at
		FROM tracker_tasks WHERE id = $1
	`, id)
	t, err := scanTrackerTask(row)
	if err != nil {
		if err == pgx.ErrNoRows {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func scanTrackerTask(s rowScanner) (*domain.TrackerTask, error) {
	var (
		t        domain.TrackerTask
		priority string
	)
	if err := s.Scan(
		&t.ID, &t.EmployeeID, &t.IntegrationID, &t.SourceTaskID,
		&t.Title, &t.Description,
		&t.Status, &priority,
		&t.Type, &t.DueAt,
		&t.EstimatedHours, &t.ActualHours,
		&t.AIEstimatedHours, &t.AIConfidence,
		&t.FetchedAt,
	); err != nil {
		return nil, err
	}
	t.Priority = domain.NormalizeTaskPriority(priority)
	return &t, nil
}

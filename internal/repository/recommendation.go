package repository

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

type RecommendationRepo struct {
	pool *pgxpool.Pool
}

func NewRecommendationRepo(pool *pgxpool.Pool) *RecommendationRepo {
	return &RecommendationRepo{pool: pool}
}

type CreateRecommendationInput struct {
	EmployeeID   *uuid.UUID
	TeamID       *uuid.UUID
	Kind         string
	Priority     domain.RecommendationPriority
	Title        string
	Explanation  string
	PayloadJSON  []byte
	GeneratedBy  string
	EvidenceJSON []byte
}

func (r *RecommendationRepo) Create(ctx context.Context, in CreateRecommendationInput) (*domain.Recommendation, error) {
	if in.EmployeeID == nil && in.TeamID == nil {
		return nil, fmt.Errorf("recommendation: employee_id or team_id required")
	}
	if in.GeneratedBy == "" {
		in.GeneratedBy = "rule"
	}
	pj := in.PayloadJSON
	if len(pj) == 0 {
		pj = []byte("{}")
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO recommendations
			(employee_id, team_id, kind, priority, title, explanation,
			 payload, generated_by, ai_evidence)
		VALUES ($1, $2, $3, $4, $5, $6, $7::jsonb, $8, $9::jsonb)
		RETURNING id, employee_id, team_id, kind, priority, title, explanation,
		          payload, status, generated_by, COALESCE(ai_evidence::text, ''),
		          created_at, updated_at
	`,
		in.EmployeeID, in.TeamID, in.Kind, string(in.Priority),
		in.Title, in.Explanation, string(pj), in.GeneratedBy,
		nullJSONb(in.EvidenceJSON))

	return scanRecommendation(row)
}

func (r *RecommendationRepo) ListByEmployee(ctx context.Context, employeeID uuid.UUID, statuses []domain.RecommendationStatus, limit int) ([]domain.Recommendation, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	statusStrs := make([]string, 0, len(statuses))
	for _, s := range statuses {
		statusStrs = append(statusStrs, string(s))
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, employee_id, team_id, kind, priority, title, explanation,
		       payload, status, generated_by, COALESCE(ai_evidence::text, ''),
		       created_at, updated_at
		FROM recommendations
		WHERE employee_id = $1
		  AND ($2::text[] IS NULL OR status::text = ANY($2::text[]))
		  AND (snoozed_until IS NULL OR snoozed_until <= now())
		ORDER BY
			CASE priority
				WHEN 'critical' THEN 0
				WHEN 'high'     THEN 1
				WHEN 'medium'   THEN 2
				ELSE 3
			END,
			created_at DESC
		LIMIT $3
	`, employeeID, statusStrsOrNil(statusStrs), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Recommendation
	for rows.Next() {
		rec, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (r *RecommendationRepo) ListAll(ctx context.Context, statuses []domain.RecommendationStatus, limit int) ([]domain.Recommendation, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	statusStrs := make([]string, 0, len(statuses))
	for _, s := range statuses {
		statusStrs = append(statusStrs, string(s))
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, employee_id, team_id, kind, priority, title, explanation,
		       payload, status, generated_by, COALESCE(ai_evidence::text, ''),
		       created_at, updated_at
		FROM recommendations
		WHERE ($1::text[] IS NULL OR status::text = ANY($1::text[]))
		  AND (snoozed_until IS NULL OR snoozed_until <= now())
		ORDER BY
			CASE priority
				WHEN 'critical' THEN 0
				WHEN 'high'     THEN 1
				WHEN 'medium'   THEN 2
				ELSE 3
			END,
			created_at DESC
		LIMIT $2
	`, statusStrsOrNil(statusStrs), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Recommendation
	for rows.Next() {
		rec, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (r *RecommendationRepo) ListByManager(ctx context.Context, managerEmployeeID uuid.UUID, statuses []domain.RecommendationStatus, limit int) ([]domain.Recommendation, error) {
	if limit <= 0 || limit > 500 {
		limit = 200
	}
	statusStrs := make([]string, 0, len(statuses))
	for _, s := range statuses {
		statusStrs = append(statusStrs, string(s))
	}
	rows, err := r.pool.Query(ctx, `
		SELECT r.id, r.employee_id, r.team_id, r.kind, r.priority, r.title, r.explanation,
		       r.payload, r.status, r.generated_by, COALESCE(r.ai_evidence::text, ''),
		       r.created_at, r.updated_at
		FROM recommendations r
		JOIN employees e ON e.id = r.employee_id
		WHERE e.manager_id = $1
		  AND ($2::text[] IS NULL OR r.status::text = ANY($2::text[]))
		  AND (r.snoozed_until IS NULL OR r.snoozed_until <= now())
		ORDER BY
			CASE r.priority
				WHEN 'critical' THEN 0
				WHEN 'high'     THEN 1
				WHEN 'medium'   THEN 2
				ELSE 3
			END,
			r.created_at DESC
		LIMIT $3
	`, managerEmployeeID, statusStrsOrNil(statusStrs), limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Recommendation
	for rows.Next() {
		rec, err := scanRecommendation(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *rec)
	}
	return out, rows.Err()
}

func (r *RecommendationRepo) Snooze(ctx context.Context, id uuid.UUID, until time.Time) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE recommendations SET snoozed_until = $2 WHERE id = $1
	`, id, until)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *RecommendationRepo) SetStatus(ctx context.Context, id uuid.UUID, status domain.RecommendationStatus) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE recommendations SET status = $2 WHERE id = $1
	`, id, string(status))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *RecommendationRepo) DeleteByEmployee(ctx context.Context, employeeID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		DELETE FROM recommendations WHERE employee_id = $1 AND status = 'new'
	`, employeeID)
	return err
}

func (r *RecommendationRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Recommendation, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, employee_id, team_id, kind, priority, title, explanation,
		       payload, status, generated_by, COALESCE(ai_evidence::text, ''),
		       created_at, updated_at
		FROM recommendations
		WHERE id = $1
	`, id)
	rec, err := scanRecommendation(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return rec, nil
}

func nullJSONb(b []byte) any {
	if len(b) == 0 {
		return nil
	}
	return string(b)
}

func statusStrsOrNil(ss []string) any {
	if len(ss) == 0 {
		return nil
	}
	return ss
}

func scanRecommendation(s rowScanner) (*domain.Recommendation, error) {
	var (
		rec      domain.Recommendation
		priority string
		status   string
		evidence string
	)
	if err := s.Scan(
		&rec.ID, &rec.EmployeeID, &rec.TeamID, &rec.Kind, &priority,
		&rec.Title, &rec.Explanation, &rec.PayloadJSON,
		&status, &rec.GeneratedBy, &evidence,
		&rec.CreatedAt, &rec.UpdatedAt,
	); err != nil {
		return nil, err
	}
	rec.Priority = domain.RecommendationPriority(priority)
	rec.Status = domain.RecommendationStatus(status)
	if evidence != "" {
		rec.EvidenceJSON = []byte(evidence)
	}
	return &rec, nil
}

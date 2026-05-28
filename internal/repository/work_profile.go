package repository

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

type WorkProfileRepo struct {
	pool *pgxpool.Pool
}

func NewWorkProfileRepo(pool *pgxpool.Pool) *WorkProfileRepo {
	return &WorkProfileRepo{pool: pool}
}

type CreateProfileInput struct {
	EmployeeID uuid.UUID
	DaysOfWeek domain.DaysOfWeek
	Timezone   string
	WorkFormat domain.WorkFormat
	Source     string
}

func (r *WorkProfileRepo) CreateNewVersion(ctx context.Context, in CreateProfileInput) (*domain.WorkProfile, error) {
	if !in.WorkFormat.Valid() {
		return nil, fmt.Errorf("invalid work_format: %s", in.WorkFormat)
	}
	if in.Source == "" {
		in.Source = "manual"
	}
	if in.Timezone == "" {
		in.Timezone = "Europe/Moscow"
	}

	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx)

	now := time.Now().UTC()

	if _, err := tx.Exec(ctx, `
		UPDATE work_profiles
		SET valid_to = $1
		WHERE employee_id = $2 AND valid_to IS NULL
	`, now, in.EmployeeID); err != nil {
		return nil, fmt.Errorf("close active: %w", err)
	}

	daysJSON, err := json.Marshal(in.DaysOfWeek)
	if err != nil {
		return nil, fmt.Errorf("marshal days: %w", err)
	}

	row := tx.QueryRow(ctx, `
		INSERT INTO work_profiles
			(employee_id, valid_from, days_of_week, timezone, work_format, source)
		VALUES ($1, $2, $3::jsonb, $4, $5, $6)
		RETURNING id, employee_id, valid_from, valid_to, days_of_week,
		          timezone, work_format, source, created_at
	`, in.EmployeeID, now, string(daysJSON), in.Timezone, string(in.WorkFormat), in.Source)

	wp, err := scanWorkProfile(row)
	if err != nil {
		return nil, fmt.Errorf("insert profile: %w", err)
	}

	if _, err := tx.Exec(ctx, `
		UPDATE employees
		SET last_profile_update_at = $1, last_confirmed_at = $1
		WHERE id = $2
	`, now, in.EmployeeID); err != nil {
		return nil, fmt.Errorf("update last_profile_update_at: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return wp, nil
}

func (r *WorkProfileRepo) Active(ctx context.Context, employeeID uuid.UUID) (*domain.WorkProfile, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, employee_id, valid_from, valid_to, days_of_week,
		       timezone, work_format, source, created_at
		FROM work_profiles
		WHERE employee_id = $1 AND valid_to IS NULL
		LIMIT 1
	`, employeeID)

	wp, err := scanWorkProfile(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("active: %w", err)
	}
	return wp, nil
}

func (r *WorkProfileRepo) History(ctx context.Context, employeeID uuid.UUID, limit int) ([]domain.WorkProfile, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, employee_id, valid_from, valid_to, days_of_week,
		       timezone, work_format, source, created_at
		FROM work_profiles
		WHERE employee_id = $1
		ORDER BY created_at DESC
		LIMIT $2
	`, employeeID, limit)
	if err != nil {
		return nil, fmt.Errorf("history: %w", err)
	}
	defer rows.Close()

	var out []domain.WorkProfile
	for rows.Next() {
		wp, err := scanWorkProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *wp)
	}
	return out, rows.Err()
}

func scanWorkProfile(s rowScanner) (*domain.WorkProfile, error) {
	var (
		wp      domain.WorkProfile
		validTo *time.Time
		daysRaw []byte
		format  string
	)
	if err := s.Scan(
		&wp.ID, &wp.EmployeeID, &wp.ValidFrom, &validTo, &daysRaw,
		&wp.Timezone, &format, &wp.Source, &wp.CreatedAt,
	); err != nil {
		return nil, err
	}
	if validTo != nil {
		wp.ValidTo = validTo
	}
	wp.WorkFormat = domain.WorkFormat(format)
	if len(daysRaw) > 0 {
		if err := json.Unmarshal(daysRaw, &wp.DaysOfWeek); err != nil {
			return nil, fmt.Errorf("unmarshal days_of_week: %w", err)
		}
	}
	return &wp, nil
}

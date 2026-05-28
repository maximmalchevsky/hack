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

type ExceptionRepo struct {
	pool *pgxpool.Pool
}

func NewExceptionRepo(pool *pgxpool.Pool) *ExceptionRepo { return &ExceptionRepo{pool: pool} }

type CreateExceptionInput struct {
	EmployeeID uuid.UUID
	Kind       domain.ExceptionKind
	StartAt    time.Time
	EndAt      time.Time
	Comment    string
	Source     string
}

func (r *ExceptionRepo) Create(ctx context.Context, in CreateExceptionInput) (*domain.TimeException, error) {
	if !in.Kind.Valid() {
		return nil, fmt.Errorf("invalid exception kind: %s", in.Kind)
	}
	if !in.EndAt.After(in.StartAt) {
		return nil, fmt.Errorf("end_at must be after start_at")
	}
	if in.Source == "" {
		in.Source = "manual"
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO time_exceptions (employee_id, kind, start_at, end_at, comment, source)
		VALUES ($1, $2, $3, $4, NULLIF($5, ''), $6)
		RETURNING id, employee_id, kind, start_at, end_at, COALESCE(comment, ''),
		          source, created_at
	`, in.EmployeeID, string(in.Kind), in.StartAt.UTC(), in.EndAt.UTC(), in.Comment, in.Source)

	return scanException(row)
}

func (r *ExceptionRepo) Delete(ctx context.Context, id, employeeID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM time_exceptions WHERE id = $1 AND employee_id = $2
	`, id, employeeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

type ListExceptionsFilter struct {
	EmployeeID uuid.UUID
	From       time.Time
	To         time.Time
}

func (r *ExceptionRepo) List(ctx context.Context, f ListExceptionsFilter) ([]domain.TimeException, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, employee_id, kind, start_at, end_at, COALESCE(comment, ''),
		       source, created_at
		FROM time_exceptions
		WHERE employee_id = $1
		  AND (
		       $2::timestamptz IS NULL
		    OR $3::timestamptz IS NULL
		    OR (start_at < $3 AND end_at > $2)
		  )
		ORDER BY start_at DESC
	`, f.EmployeeID, nullTime(f.From), nullTime(f.To))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TimeException
	for rows.Next() {
		e, err := scanException(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *e)
	}
	return out, rows.Err()
}

func (r *ExceptionRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.TimeException, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, employee_id, kind, start_at, end_at, COALESCE(comment, ''),
		       source, created_at
		FROM time_exceptions
		WHERE id = $1
	`, id)
	e, err := scanException(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return e, nil
}

func scanException(s rowScanner) (*domain.TimeException, error) {
	var (
		e    domain.TimeException
		kind string
	)
	if err := s.Scan(
		&e.ID, &e.EmployeeID, &kind, &e.StartAt, &e.EndAt, &e.Comment,
		&e.Source, &e.CreatedAt,
	); err != nil {
		return nil, err
	}
	e.Kind = domain.ExceptionKind(kind)
	return &e, nil
}

func nullTime(t time.Time) any {
	if t.IsZero() {
		return nil
	}
	return t.UTC()
}

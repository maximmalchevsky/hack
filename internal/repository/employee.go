package repository

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

// EmployeeRepo — доступ к employees.
type EmployeeRepo struct {
	pool *pgxpool.Pool
}

func NewEmployeeRepo(pool *pgxpool.Pool) *EmployeeRepo { return &EmployeeRepo{pool: pool} }

// Create — создаёт минимальную запись employee для нового user.
func (r *EmployeeRepo) Create(ctx context.Context, userID uuid.UUID) (*domain.Employee, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO employees (user_id)
		VALUES ($1)
		RETURNING id, user_id, COALESCE(department, ''), COALESCE(position, ''),
		          hr_work_format, hire_date, last_profile_update_at, last_confirmed_at,
		          manager_id, created_at, updated_at
	`, userID)

	emp, err := scanEmployee(row)
	if err != nil {
		return nil, fmt.Errorf("employee repo: create: %w", err)
	}
	return emp, nil
}

// ByUserID — получить employee по user_id.
func (r *EmployeeRepo) ByUserID(ctx context.Context, userID uuid.UUID) (*domain.Employee, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, user_id, COALESCE(department, ''), COALESCE(position, ''),
		       hr_work_format, hire_date, last_profile_update_at, last_confirmed_at,
		       manager_id, created_at, updated_at
		FROM employees
		WHERE user_id = $1
	`, userID)

	emp, err := scanEmployee(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("employee repo: by_user_id: %w", err)
	}
	return emp, nil
}

func scanEmployee(s rowScanner) (*domain.Employee, error) {
	var (
		emp         domain.Employee
		format      *string
	)
	if err := s.Scan(
		&emp.ID, &emp.UserID, &emp.Department, &emp.Position,
		&format, &emp.HireDate, &emp.LastProfileUpdateAt, &emp.LastConfirmedAt,
		&emp.ManagerID, &emp.CreatedAt, &emp.UpdatedAt,
	); err != nil {
		return nil, err
	}
	if format != nil {
		wf := domain.WorkFormat(*format)
		emp.HRWorkFormat = &wf
	}
	return &emp, nil
}

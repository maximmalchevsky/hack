package service

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

type EmployeeService struct {
	pool     *pgxpool.Pool
	users    *repository.UserRepo
	emps     *repository.EmployeeRepo
	profiles *repository.WorkProfileRepo
	excs     *repository.ExceptionRepo
	events   *repository.CalendarEventRepo
	integ    *repository.IntegrationRepo
}

func NewEmployeeService(pool *pgxpool.Pool) *EmployeeService {
	return &EmployeeService{
		pool:     pool,
		users:    repository.NewUserRepo(pool),
		emps:     repository.NewEmployeeRepo(pool),
		profiles: repository.NewWorkProfileRepo(pool),
		excs:     repository.NewExceptionRepo(pool),
		events:   repository.NewCalendarEventRepo(pool),
		integ:    repository.NewIntegrationRepo(pool),
	}
}

type EmployeeListRow struct {
	EmployeeID          uuid.UUID  `json:"employee_id"`
	UserID              uuid.UUID  `json:"user_id"`
	Email               string     `json:"email"`
	FullName            string     `json:"full_name"`
	Role                string     `json:"role"`
	Department          string     `json:"department,omitempty"`
	Position            string     `json:"position,omitempty"`
	Timezone            string     `json:"timezone,omitempty"`
	HRWorkFormat        string     `json:"hr_work_format,omitempty"`
	LastProfileUpdateAt *time.Time `json:"last_profile_update_at,omitempty"`
}

type EmployeeDetail struct {
	Employee     EmployeeListRow        `json:"employee"`
	WorkProfile  *domain.WorkProfile    `json:"work_profile,omitempty"`
	Exceptions   []domain.TimeException `json:"exceptions"`
	Integrations []IntegrationListRow   `json:"integrations"`
	UpcomingEvts int                    `json:"upcoming_events_count"`
}

type IntegrationListRow struct {
	ID           uuid.UUID  `json:"id"`
	Provider     string     `json:"provider"`
	AccountEmail string     `json:"account_email,omitempty"`
	AccountLabel string     `json:"account_label,omitempty"`
	Status       string     `json:"status"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
}

func (s *EmployeeService) List(ctx context.Context) ([]EmployeeListRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT e.id, e.user_id, u.email, u.full_name, u.role,
		       COALESCE(e.department, ''), COALESCE(e.position, ''),
		       COALESCE(wp.timezone, u.timezone),
		       e.hr_work_format, e.last_profile_update_at
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		ORDER BY u.full_name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []EmployeeListRow
	for rows.Next() {
		var (
			r        EmployeeListRow
			hrFormat *string
			lastUpd  *time.Time
		)
		if err := rows.Scan(&r.EmployeeID, &r.UserID, &r.Email, &r.FullName, &r.Role,
			&r.Department, &r.Position, &r.Timezone, &hrFormat, &lastUpd); err != nil {
			return nil, err
		}
		if hrFormat != nil {
			r.HRWorkFormat = *hrFormat
		}
		r.LastProfileUpdateAt = lastUpd
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *EmployeeService) Detail(ctx context.Context, employeeID uuid.UUID) (*EmployeeDetail, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT e.id, e.user_id, u.email, u.full_name, u.role,
		       COALESCE(e.department, ''), COALESCE(e.position, ''),
		       COALESCE(wp.timezone, u.timezone),
		       e.hr_work_format, e.last_profile_update_at
		FROM employees e
		JOIN users u ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		WHERE e.id = $1
	`, employeeID)

	var (
		base     EmployeeListRow
		hrFormat *string
		lastUpd  *time.Time
	)
	if err := row.Scan(&base.EmployeeID, &base.UserID, &base.Email, &base.FullName, &base.Role,
		&base.Department, &base.Position, &base.Timezone, &hrFormat, &lastUpd); err != nil {
		return nil, fmt.Errorf("employee detail: %w", err)
	}
	if hrFormat != nil {
		base.HRWorkFormat = *hrFormat
	}
	base.LastProfileUpdateAt = lastUpd

	detail := &EmployeeDetail{Employee: base}

	if wp, err := s.profiles.Active(ctx, employeeID); err == nil && wp != nil {
		detail.WorkProfile = wp
	}

	excs, _ := s.excs.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: employeeID,
		From:       time.Now().UTC().AddDate(0, 0, -30),
		To:         time.Now().UTC().AddDate(0, 0, 90),
	})
	detail.Exceptions = excs

	integs, _ := s.integ.ListByEmployee(ctx, employeeID)
	for _, i := range integs {
		detail.Integrations = append(detail.Integrations, IntegrationListRow{
			ID:           i.ID,
			Provider:     string(i.Provider),
			AccountEmail: i.AccountEmail,
			AccountLabel: i.AccountLabel,
			Status:       string(i.Status),
			LastSyncAt:   i.LastSyncAt,
			LastError:    i.LastError,
		})
	}

	if cnt, err := s.events.Count(ctx, employeeID, time.Now().UTC(), time.Now().UTC().AddDate(0, 0, 14)); err == nil {
		detail.UpcomingEvts = cnt
	}

	return detail, nil
}

var ErrEmployeeNotFound = errors.New("employee not found")

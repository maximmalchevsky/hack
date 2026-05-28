package service

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

type AdminService struct {
	pool *pgxpool.Pool
}

func NewAdminService(pool *pgxpool.Pool) *AdminService {
	return &AdminService{pool: pool}
}

type AdminUserRow struct {
	ID        uuid.UUID `json:"id"`
	Email     string    `json:"email"`
	Role      string    `json:"role"`
	FullName  string    `json:"full_name"`
	Timezone  string    `json:"timezone"`
	CreatedAt time.Time `json:"created_at"`
}

func (s *AdminService) ListUsers(ctx context.Context) ([]AdminUserRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, email, role, full_name, timezone, created_at
		FROM users
		ORDER BY created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AdminUserRow
	for rows.Next() {
		var r AdminUserRow
		if err := rows.Scan(&r.ID, &r.Email, &r.Role, &r.FullName, &r.Timezone, &r.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

var ErrUserNotFound = errors.New("admin: user not found")

func (s *AdminService) UpdateEmail(ctx context.Context, userID uuid.UUID, newEmail string) error {
	email := strings.ToLower(strings.TrimSpace(newEmail))
	if !looksLikeEmailService(email) {
		return ErrInvalidEmail
	}
	users := repository.NewUserRepo(s.pool)
	if err := users.UpdateEmail(ctx, userID, email); err != nil {
		if errors.Is(err, repository.ErrEmailTaken) {
			return ErrEmailTaken
		}
		if errors.Is(err, repository.ErrNotFound) {
			return ErrUserNotFound
		}
		return err
	}
	return nil
}

func looksLikeEmailService(s string) bool {
	if len(s) < 3 || len(s) > 254 {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	dot := strings.LastIndexByte(s, '.')
	if dot < at+2 || dot == len(s)-1 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == ',' || r == ';' || r == '"' || r == '\'' {
			return false
		}
	}
	return true
}

func (s *AdminService) UpdateRole(ctx context.Context, userID uuid.UUID, newRole domain.Role) error {
	if !newRole.Valid() {
		return errors.New("invalid role")
	}
	tag, err := s.pool.Exec(ctx, `
		UPDATE users SET role = $2 WHERE id = $1
	`, userID, string(newRole))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("user not found")
	}
	return nil
}

type AdminIntegrationRow struct {
	ID           uuid.UUID  `json:"id"`
	EmployeeID   uuid.UUID  `json:"employee_id"`
	EmployeeName string     `json:"employee_name"`
	Provider     string     `json:"provider"`
	Status       string     `json:"status"`
	AccountLabel string     `json:"account_label,omitempty"`
	AccountEmail string     `json:"account_email,omitempty"`
	LastSyncAt   *time.Time `json:"last_sync_at,omitempty"`
	LastError    string     `json:"last_error,omitempty"`
	CreatedAt    time.Time  `json:"created_at"`
}

func (s *AdminService) ListIntegrations(ctx context.Context) ([]AdminIntegrationRow, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT i.id, i.employee_id, u.full_name,
		       i.provider, i.status,
		       COALESCE(i.account_label, ''), COALESCE(i.account_email, ''),
		       i.last_sync_at, COALESCE(i.last_error, ''),
		       i.created_at
		FROM integrations i
		JOIN employees e ON e.id = i.employee_id
		JOIN users u ON u.id = e.user_id
		ORDER BY i.created_at DESC
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AdminIntegrationRow
	for rows.Next() {
		var r AdminIntegrationRow
		if err := rows.Scan(
			&r.ID, &r.EmployeeID, &r.EmployeeName,
			&r.Provider, &r.Status,
			&r.AccountLabel, &r.AccountEmail,
			&r.LastSyncAt, &r.LastError,
			&r.CreatedAt,
		); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

type AnalyticsWeights struct {
	W1             float64   `json:"w1"`
	W2             float64   `json:"w2"`
	W3             float64   `json:"w3"`
	W4             float64   `json:"w4"`
	W5             float64   `json:"w5"`
	FreshnessDDays int       `json:"freshness_d_days"`
	UpdatedAt      time.Time `json:"updated_at"`
}

func (s *AdminService) GetWeights(ctx context.Context) (*AnalyticsWeights, error) {
	var w AnalyticsWeights
	err := s.pool.QueryRow(ctx, `
		SELECT w1, w2, w3, w4, w5, freshness_d_days, updated_at
		FROM analytics_weights WHERE id = 1
	`).Scan(&w.W1, &w.W2, &w.W3, &w.W4, &w.W5, &w.FreshnessDDays, &w.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &w, nil
}

func (s *AdminService) UpdateWeights(ctx context.Context, w AnalyticsWeights) error {
	total := w.W1 + w.W2 + w.W3 + w.W4 + w.W5
	if total < 0.99 || total > 1.01 {
		return fmt.Errorf("weights must sum to 1.0, got %.3f", total)
	}
	if w.FreshnessDDays <= 0 || w.FreshnessDDays > 365 {
		return errors.New("freshness_d_days must be in (0, 365]")
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE analytics_weights
		SET w1 = $1, w2 = $2, w3 = $3, w4 = $4, w5 = $5,
		    freshness_d_days = $6, updated_at = now()
		WHERE id = 1
	`, w.W1, w.W2, w.W3, w.W4, w.W5, w.FreshnessDDays)
	return err
}

type SystemHealth struct {
	UsersCount         int `json:"users_count"`
	EmployeesCount     int `json:"employees_count"`
	TeamsCount         int `json:"teams_count"`
	IntegrationsActive int `json:"integrations_active"`
	IntegrationsError  int `json:"integrations_error"`
	UnreadNotifs       int `json:"unread_notifications"`
	WebhookInboxQueued int `json:"webhook_inbox_queued"`
}

func (s *AdminService) Health(ctx context.Context) (*SystemHealth, error) {
	h := &SystemHealth{}
	q := func(sql string) (int, error) {
		var n int
		err := s.pool.QueryRow(ctx, sql).Scan(&n)
		return n, err
	}
	var err error
	if h.UsersCount, err = q(`SELECT COUNT(*) FROM users`); err != nil {
		return nil, err
	}
	if h.EmployeesCount, err = q(`SELECT COUNT(*) FROM employees`); err != nil {
		return nil, err
	}
	if h.TeamsCount, err = q(`SELECT COUNT(*) FROM teams`); err != nil {
		return nil, err
	}
	if h.IntegrationsActive, err = q(`SELECT COUNT(*) FROM integrations WHERE status = 'connected'`); err != nil {
		return nil, err
	}
	if h.IntegrationsError, err = q(`SELECT COUNT(*) FROM integrations WHERE status = 'error'`); err != nil {
		return nil, err
	}
	if h.UnreadNotifs, err = q(`SELECT COUNT(*) FROM notifications WHERE read_at IS NULL`); err != nil {
		return nil, err
	}
	if h.WebhookInboxQueued, err = q(`SELECT COUNT(*) FROM webhook_inbox WHERE processed_at IS NULL`); err != nil {
		return nil, err
	}
	return h, nil
}

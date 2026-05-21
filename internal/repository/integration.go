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

type IntegrationRepo struct {
	pool *pgxpool.Pool
}

func NewIntegrationRepo(pool *pgxpool.Pool) *IntegrationRepo {
	return &IntegrationRepo{pool: pool}
}

type CreateIntegrationInput struct {
	EmployeeID      uuid.UUID
	Provider        domain.IntegrationProvider
	AccountEmail    string
	AccountLabel    string
	AccessTokenEnc  string
	RefreshTokenEnc string
	ExpiresAt       *time.Time
	ConfigJSON      []byte // optional
}

func (r *IntegrationRepo) Create(ctx context.Context, in CreateIntegrationInput) (*domain.Integration, error) {
	if !in.Provider.Valid() {
		return nil, fmt.Errorf("invalid provider: %s", in.Provider)
	}
	cfg := in.ConfigJSON
	if len(cfg) == 0 {
		cfg = []byte("{}")
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO integrations
			(employee_id, provider, account_email, account_label,
			 access_token_enc, refresh_token_enc, expires_at, status, config)
		VALUES ($1, $2, NULLIF($3, ''), NULLIF($4, ''), NULLIF($5, ''), NULLIF($6, ''),
		        $7, 'connected', $8::jsonb)
		RETURNING id, employee_id, provider, COALESCE(account_email, ''),
		          COALESCE(account_label, ''), COALESCE(access_token_enc, ''),
		          COALESCE(refresh_token_enc, ''), expires_at, status,
		          last_sync_at, COALESCE(last_error, ''),
		          COALESCE(webhook_sub_id, ''), config, created_at, updated_at
	`,
		in.EmployeeID, string(in.Provider), in.AccountEmail, in.AccountLabel,
		in.AccessTokenEnc, in.RefreshTokenEnc, in.ExpiresAt, string(cfg))

	return scanIntegration(row)
}

func (r *IntegrationRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	row := r.pool.QueryRow(ctx, integrationSelectByPK, id)
	i, err := scanIntegration(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return i, nil
}

func (r *IntegrationRepo) ListByEmployee(ctx context.Context, employeeID uuid.UUID) ([]domain.Integration, error) {
	rows, err := r.pool.Query(ctx, `
		`+integrationCols+`
		FROM integrations
		WHERE employee_id = $1
		ORDER BY created_at DESC
	`, employeeID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// ListActive — все интеграции в статусе connected (для cron-sync).
func (r *IntegrationRepo) ListActive(ctx context.Context) ([]domain.Integration, error) {
	rows, err := r.pool.Query(ctx, `
		`+integrationCols+`
		FROM integrations
		WHERE status = 'connected'
		ORDER BY last_sync_at NULLS FIRST
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Integration
	for rows.Next() {
		i, err := scanIntegration(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *i)
	}
	return out, rows.Err()
}

// MarkSyncSuccess — отметка успешной синхронизации.
func (r *IntegrationRepo) MarkSyncSuccess(ctx context.Context, id uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE integrations
		SET last_sync_at = now(), status = 'connected', last_error = NULL
		WHERE id = $1
	`, id)
	return err
}

// MarkSyncError — отметка ошибки.
func (r *IntegrationRepo) MarkSyncError(ctx context.Context, id uuid.UUID, errMsg string) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE integrations
		SET last_sync_at = now(), status = 'error', last_error = $2
		WHERE id = $1
	`, id, errMsg)
	return err
}

// UpdateTokens — обновляет access/refresh/expires после OAuth refresh.
func (r *IntegrationRepo) UpdateTokens(ctx context.Context, id uuid.UUID, accessEnc, refreshEnc string, expiresAt time.Time) error {
	var expPtr *time.Time
	if !expiresAt.IsZero() {
		t := expiresAt
		expPtr = &t
	}
	_, err := r.pool.Exec(ctx, `
		UPDATE integrations
		SET access_token_enc = $2,
		    refresh_token_enc = COALESCE(NULLIF($3, ''), refresh_token_enc),
		    expires_at = $4,
		    updated_at = now()
		WHERE id = $1
	`, id, accessEnc, refreshEnc, expPtr)
	return err
}

func (r *IntegrationRepo) Delete(ctx context.Context, id, employeeID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM integrations WHERE id = $1 AND employee_id = $2
	`, id, employeeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// --- helpers ---

const integrationCols = `
	SELECT id, employee_id, provider, COALESCE(account_email, ''),
	       COALESCE(account_label, ''), COALESCE(access_token_enc, ''),
	       COALESCE(refresh_token_enc, ''), expires_at, status,
	       last_sync_at, COALESCE(last_error, ''),
	       COALESCE(webhook_sub_id, ''), config, created_at, updated_at
`

const integrationSelectByPK = integrationCols + `
	FROM integrations
	WHERE id = $1
`

func scanIntegration(s rowScanner) (*domain.Integration, error) {
	var (
		i        domain.Integration
		provider string
		status   string
		config   []byte
	)
	if err := s.Scan(
		&i.ID, &i.EmployeeID, &provider, &i.AccountEmail, &i.AccountLabel,
		&i.AccessTokenEnc, &i.RefreshTokenEnc, &i.ExpiresAt, &status,
		&i.LastSyncAt, &i.LastError, &i.WebhookSubID, &config,
		&i.CreatedAt, &i.UpdatedAt,
	); err != nil {
		return nil, err
	}
	i.Provider = domain.IntegrationProvider(provider)
	i.Status = domain.IntegrationStatus(status)
	i.ConfigJSON = config
	return &i, nil
}

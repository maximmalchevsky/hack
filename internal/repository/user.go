package repository

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

type UserRepo struct {
	pool *pgxpool.Pool
}

func NewUserRepo(pool *pgxpool.Pool) *UserRepo { return &UserRepo{pool: pool} }

type CreateUserInput struct {
	Email        string
	PasswordHash string
	Role         domain.Role
	FullName     string
	Timezone     string
	Locale       string
}

var ErrEmailTaken = errors.New("repository: email already taken")

func (r *UserRepo) Create(ctx context.Context, in CreateUserInput) (*domain.User, error) {
	tz := in.Timezone
	if tz == "" {
		tz = "Europe/Moscow"
	}
	locale := in.Locale
	if locale == "" {
		locale = "ru"
	}

	row := r.pool.QueryRow(ctx, `
		INSERT INTO users (email, password_hash, role, full_name, timezone, locale)
		VALUES ($1, $2, $3, $4, $5, $6)
		RETURNING id, email, password_hash, role, full_name, timezone, locale,
		          COALESCE(avatar_url, ''), created_at, updated_at
	`, strings.ToLower(in.Email), in.PasswordHash, string(in.Role), in.FullName, tz, locale)

	u, err := scanUser(row)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return nil, ErrEmailTaken
		}
		return nil, fmt.Errorf("user repo: create: %w", err)
	}
	return u, nil
}

func (r *UserRepo) ByEmail(ctx context.Context, email string) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, full_name, timezone, locale,
		       COALESCE(avatar_url, ''), created_at, updated_at
		FROM users
		WHERE email = $1
	`, strings.ToLower(email))

	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user repo: by_email: %w", err)
	}
	return u, nil
}

func (r *UserRepo) UpdateEmail(ctx context.Context, id uuid.UUID, newEmail string) error {
	email := strings.ToLower(strings.TrimSpace(newEmail))
	if email == "" {
		return fmt.Errorf("user repo: empty email")
	}
	tag, err := r.pool.Exec(ctx, `
		UPDATE users SET email = $1, updated_at = now()
		WHERE id = $2
	`, email, id)
	if err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrEmailTaken
		}
		return fmt.Errorf("user repo: update email: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *UserRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.User, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, email, password_hash, role, full_name, timezone, locale,
		       COALESCE(avatar_url, ''), created_at, updated_at
		FROM users
		WHERE id = $1
	`, id)

	u, err := scanUser(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("user repo: by_id: %w", err)
	}
	return u, nil
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanUser(s rowScanner) (*domain.User, error) {
	var (
		u       domain.User
		role    string
		created time.Time
		updated time.Time
	)
	if err := s.Scan(
		&u.ID, &u.Email, &u.PasswordHash, &role, &u.FullName,
		&u.Timezone, &u.Locale, &u.AvatarURL, &created, &updated,
	); err != nil {
		return nil, err
	}
	u.Role = domain.Role(role)
	u.CreatedAt = created
	u.UpdatedAt = updated
	return &u, nil
}

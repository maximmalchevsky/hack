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

type NotificationRepo struct {
	pool *pgxpool.Pool
}

func NewNotificationRepo(pool *pgxpool.Pool) *NotificationRepo {
	return &NotificationRepo{pool: pool}
}

type CreateNotificationInput struct {
	UserID  uuid.UUID
	Kind    string
	Title   string
	Body    string
	Link    string
	Payload []byte
}

func (r *NotificationRepo) Create(ctx context.Context, in CreateNotificationInput) (*domain.Notification, error) {
	payload := in.Payload
	if len(payload) == 0 {
		payload = []byte("{}")
	}
	row := r.pool.QueryRow(ctx, `
		INSERT INTO notifications (user_id, kind, title, body, link, payload)
		VALUES ($1, $2, $3, NULLIF($4, ''), NULLIF($5, ''), $6::jsonb)
		RETURNING id, user_id, kind, title, COALESCE(body, ''), COALESCE(link, ''),
		          payload, read_at, created_at
	`, in.UserID, in.Kind, in.Title, in.Body, in.Link, string(payload))

	return scanNotification(row)
}

// ListByUser — список с возможным фильтром по непрочитанным.
func (r *NotificationRepo) ListByUser(ctx context.Context, userID uuid.UUID, onlyUnread bool, limit int) ([]domain.Notification, error) {
	if limit <= 0 || limit > 200 {
		limit = 50
	}
	q := `
		SELECT id, user_id, kind, title, COALESCE(body, ''), COALESCE(link, ''),
		       payload, read_at, created_at
		FROM notifications
		WHERE user_id = $1
	`
	if onlyUnread {
		q += " AND read_at IS NULL"
	}
	q += " ORDER BY created_at DESC LIMIT $2"

	rows, err := r.pool.Query(ctx, q, userID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Notification
	for rows.Next() {
		n, err := scanNotification(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *n)
	}
	return out, rows.Err()
}

// CountUnread — счётчик для бейджа.
func (r *NotificationRepo) CountUnread(ctx context.Context, userID uuid.UUID) (int, error) {
	var n int
	err := r.pool.QueryRow(ctx, `
		SELECT COUNT(*) FROM notifications WHERE user_id = $1 AND read_at IS NULL
	`, userID).Scan(&n)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return 0, nil
		}
		return 0, err
	}
	return n, nil
}

func (r *NotificationRepo) MarkRead(ctx context.Context, id, userID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE id = $1 AND user_id = $2 AND read_at IS NULL
	`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (r *NotificationRepo) MarkAllRead(ctx context.Context, userID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE notifications SET read_at = now()
		WHERE user_id = $1 AND read_at IS NULL
	`, userID)
	return err
}

// --- helpers ---

func scanNotification(s rowScanner) (*domain.Notification, error) {
	var (
		n      domain.Notification
		readAt *time.Time
	)
	if err := s.Scan(
		&n.ID, &n.UserID, &n.Kind, &n.Title, &n.Body, &n.Link,
		&n.Payload, &readAt, &n.CreatedAt,
	); err != nil {
		return nil, fmt.Errorf("scan notification: %w", err)
	}
	n.ReadAt = readAt
	return &n, nil
}

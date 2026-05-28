package service

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ViewPresetsService struct {
	pool *pgxpool.Pool
}

func NewViewPresetsService(pool *pgxpool.Pool) *ViewPresetsService {
	return &ViewPresetsService{pool: pool}
}

type ViewPreset struct {
	ID        uuid.UUID       `json:"id"`
	Page      string          `json:"page"`
	Name      string          `json:"name"`
	Filters   json.RawMessage `json:"filters"`
	CreatedAt time.Time       `json:"created_at"`
}

var ErrInvalidPage = errors.New("view-preset: unknown page")

func validPage(p string) bool {
	return p == "analytics" || p == "diagnostics"
}

func (s *ViewPresetsService) List(ctx context.Context, userID uuid.UUID, page string) ([]ViewPreset, error) {
	if !validPage(page) {
		return nil, ErrInvalidPage
	}
	rows, err := s.pool.Query(ctx, `
		SELECT id, page, name, filters, created_at
		FROM view_presets
		WHERE user_id = $1 AND page = $2
		ORDER BY created_at DESC
	`, userID, page)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []ViewPreset{}
	for rows.Next() {
		var p ViewPreset
		if err := rows.Scan(&p.ID, &p.Page, &p.Name, &p.Filters, &p.CreatedAt); err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *ViewPresetsService) Create(ctx context.Context, userID uuid.UUID, page, name string, filters json.RawMessage) (ViewPreset, error) {
	if !validPage(page) {
		return ViewPreset{}, ErrInvalidPage
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return ViewPreset{}, errors.New("name is required")
	}
	if len(filters) == 0 {
		filters = []byte("{}")
	}
	var p ViewPreset
	err := s.pool.QueryRow(ctx, `
		INSERT INTO view_presets (user_id, page, name, filters)
		VALUES ($1, $2, $3, $4)
		RETURNING id, page, name, filters, created_at
	`, userID, page, name, filters).Scan(&p.ID, &p.Page, &p.Name, &p.Filters, &p.CreatedAt)
	return p, err
}

func (s *ViewPresetsService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM view_presets WHERE id = $1 AND user_id = $2`, id, userID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return errors.New("not found")
	}
	return nil
}

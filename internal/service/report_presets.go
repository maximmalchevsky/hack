// Package service — ReportPresetService: CRUD сохранённых пользователем
// конфигураций отчётов для /reports/builder.
package service

import (
	"context"
	"encoding/json"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type ReportPresetService struct {
	pool *pgxpool.Pool
}

func NewReportPresetService(pool *pgxpool.Pool) *ReportPresetService {
	return &ReportPresetService{pool: pool}
}

// ReportPresetFilters — JSON-структура filters в БД.
// Все поля optional. Пустой массив departments = без фильтра.
type ReportPresetFilters struct {
	From        *time.Time `json:"from,omitempty"`
	To          *time.Time `json:"to,omitempty"`
	Departments []string   `json:"departments,omitempty"`
}

// ReportPreset — одна сохранённая конфигурация.
type ReportPreset struct {
	ID        uuid.UUID           `json:"id"`
	UserID    uuid.UUID           `json:"user_id"`
	Name      string              `json:"name"`
	Kind      string              `json:"kind"`
	Columns   []string            `json:"columns"`
	Filters   ReportPresetFilters `json:"filters"`
	CreatedAt time.Time           `json:"created_at"`
	UpdatedAt time.Time           `json:"updated_at"`
}

var (
	ErrPresetNotFound  = errors.New("report preset: not found")
	ErrPresetForbidden = errors.New("report preset: forbidden")
	ErrPresetInvalid   = errors.New("report preset: invalid")
)

// validKinds — допустимые источники.
var validKinds = map[string]struct{}{
	"upcoming_vacations": {},
	"stale_profiles":     {},
	"conflicts":          {},
	"all_employees":      {},
}

func validateKind(k string) error {
	if _, ok := validKinds[k]; !ok {
		return ErrPresetInvalid
	}
	return nil
}

// List — все пресеты пользователя.
func (s *ReportPresetService) List(ctx context.Context, userID uuid.UUID) ([]ReportPreset, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT id, user_id, name, kind, columns, filters, created_at, updated_at
		FROM report_presets
		WHERE user_id = $1
		ORDER BY name
	`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	out := []ReportPreset{}
	for rows.Next() {
		p, err := scanPreset(rows)
		if err != nil {
			continue
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Get — один пресет (с проверкой owner).
func (s *ReportPresetService) Get(ctx context.Context, id, userID uuid.UUID) (*ReportPreset, error) {
	row := s.pool.QueryRow(ctx, `
		SELECT id, user_id, name, kind, columns, filters, created_at, updated_at
		FROM report_presets WHERE id = $1
	`, id)
	p, err := scanPreset(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrPresetNotFound
		}
		return nil, err
	}
	if p.UserID != userID {
		return nil, ErrPresetForbidden
	}
	return &p, nil
}

// Create — новая запись.
func (s *ReportPresetService) Create(ctx context.Context, userID uuid.UUID, p ReportPreset) (*ReportPreset, error) {
	if p.Name == "" {
		return nil, ErrPresetInvalid
	}
	if err := validateKind(p.Kind); err != nil {
		return nil, err
	}
	colsJSON, err := json.Marshal(p.Columns)
	if err != nil {
		return nil, err
	}
	filJSON, err := json.Marshal(p.Filters)
	if err != nil {
		return nil, err
	}
	var id uuid.UUID
	if err := s.pool.QueryRow(ctx, `
		INSERT INTO report_presets (user_id, name, kind, columns, filters)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id
	`, userID, p.Name, p.Kind, colsJSON, filJSON).Scan(&id); err != nil {
		return nil, err
	}
	return s.Get(ctx, id, userID)
}

// Update — изменение существующего (только своего).
func (s *ReportPresetService) Update(ctx context.Context, id, userID uuid.UUID, p ReportPreset) (*ReportPreset, error) {
	existing, err := s.Get(ctx, id, userID)
	if err != nil {
		return nil, err
	}
	if p.Name == "" {
		p.Name = existing.Name
	}
	if p.Kind == "" {
		p.Kind = existing.Kind
	}
	if err := validateKind(p.Kind); err != nil {
		return nil, err
	}
	colsJSON, err := json.Marshal(p.Columns)
	if err != nil {
		return nil, err
	}
	filJSON, err := json.Marshal(p.Filters)
	if err != nil {
		return nil, err
	}
	if _, err := s.pool.Exec(ctx, `
		UPDATE report_presets
		SET name = $1, kind = $2, columns = $3, filters = $4, updated_at = now()
		WHERE id = $5
	`, p.Name, p.Kind, colsJSON, filJSON, id); err != nil {
		return nil, err
	}
	return s.Get(ctx, id, userID)
}

// Delete — удаление своего.
func (s *ReportPresetService) Delete(ctx context.Context, id, userID uuid.UUID) error {
	// Сначала убеждаемся что наш.
	if _, err := s.Get(ctx, id, userID); err != nil {
		return err
	}
	_, err := s.pool.Exec(ctx, `DELETE FROM report_presets WHERE id = $1`, id)
	return err
}

// scanPreset — общий разбор строки.
type pgxScanner interface {
	Scan(dest ...any) error
}

func scanPreset(s pgxScanner) (ReportPreset, error) {
	var (
		p              ReportPreset
		colsRaw, filRaw []byte
	)
	if err := s.Scan(&p.ID, &p.UserID, &p.Name, &p.Kind, &colsRaw, &filRaw, &p.CreatedAt, &p.UpdatedAt); err != nil {
		return p, err
	}
	if len(colsRaw) > 0 {
		_ = json.Unmarshal(colsRaw, &p.Columns)
	}
	if p.Columns == nil {
		p.Columns = []string{}
	}
	if len(filRaw) > 0 {
		_ = json.Unmarshal(filRaw, &p.Filters)
	}
	return p, nil
}

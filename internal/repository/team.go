package repository

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
)

type TeamRepo struct {
	pool *pgxpool.Pool
}

func NewTeamRepo(pool *pgxpool.Pool) *TeamRepo { return &TeamRepo{pool: pool} }

func (r *TeamRepo) List(ctx context.Context) ([]domain.Team, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT id, name, owner_id, created_at, updated_at
		FROM teams
		ORDER BY name
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.Team
	for rows.Next() {
		t, err := scanTeam(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *t)
	}
	return out, rows.Err()
}

func (r *TeamRepo) ByID(ctx context.Context, id uuid.UUID) (*domain.Team, error) {
	row := r.pool.QueryRow(ctx, `
		SELECT id, name, owner_id, created_at, updated_at
		FROM teams WHERE id = $1
	`, id)
	t, err := scanTeam(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *TeamRepo) Members(ctx context.Context, teamID uuid.UUID) ([]domain.TeamMemberDetailed, error) {
	rows, err := r.pool.Query(ctx, `
		SELECT tm.team_id, tm.employee_id, u.full_name, u.role,
		       COALESCE(e.department, ''),
		       COALESCE(wp.timezone, ''),
		       COALESCE(wp.work_format::text, ''),
		       e.last_profile_update_at
		FROM team_members tm
		JOIN employees e ON e.id = tm.employee_id
		JOIN users u     ON u.id = e.user_id
		LEFT JOIN work_profiles wp ON wp.employee_id = e.id AND wp.valid_to IS NULL
		WHERE tm.team_id = $1
		ORDER BY u.full_name
	`, teamID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []domain.TeamMemberDetailed
	for rows.Next() {
		var (
			m          domain.TeamMemberDetailed
			role       string
			lastUpdate *time.Time
		)
		if err := rows.Scan(
			&m.TeamID, &m.EmployeeID, &m.FullName, &role, &m.Department,
			&m.Timezone, &m.WorkFormat, &lastUpdate,
		); err != nil {
			return nil, err
		}
		m.Role = domain.Role(role)
		m.LastProfileUpdateAt = lastUpdate
		out = append(out, m)
	}
	return out, rows.Err()
}

// Create — добавляет команду. ownerID может быть nil.
func (r *TeamRepo) Create(ctx context.Context, name string, ownerID *uuid.UUID) (*domain.Team, error) {
	row := r.pool.QueryRow(ctx, `
		INSERT INTO teams (name, owner_id) VALUES ($1, $2)
		RETURNING id, name, owner_id, created_at, updated_at
	`, name, ownerID)
	return scanTeam(row)
}

// Update — переименование и/или смена владельца.
func (r *TeamRepo) Update(ctx context.Context, id uuid.UUID, name *string, ownerID *uuid.UUID, ownerSet bool) (*domain.Team, error) {
	// собираем динамический SET — нельзя обновлять nil-полем безусловно.
	q := `UPDATE teams SET updated_at = now()`
	args := []any{id}
	if name != nil {
		args = append(args, *name)
		q += `, name = $` + itoa(len(args))
	}
	if ownerSet {
		args = append(args, ownerID) // может быть nil → SET NULL
		q += `, owner_id = $` + itoa(len(args))
	}
	q += ` WHERE id = $1 RETURNING id, name, owner_id, created_at, updated_at`

	row := r.pool.QueryRow(ctx, q, args...)
	t, err := scanTeam(row)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrNotFound
		}
		return nil, err
	}
	return t, nil
}

func (r *TeamRepo) Delete(ctx context.Context, id uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `DELETE FROM teams WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// AddMember — идемпотентно (ON CONFLICT DO NOTHING).
func (r *TeamRepo) AddMember(ctx context.Context, teamID, employeeID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO team_members (team_id, employee_id) VALUES ($1, $2)
		ON CONFLICT DO NOTHING
	`, teamID, employeeID)
	return err
}

func (r *TeamRepo) RemoveMember(ctx context.Context, teamID, employeeID uuid.UUID) error {
	tag, err := r.pool.Exec(ctx, `
		DELETE FROM team_members WHERE team_id = $1 AND employee_id = $2
	`, teamID, employeeID)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// SetManagerForMembers — назначает managerID руководителем всех участников
// команды, кроме самого managerID. Используется со страницы /teams чтобы
// заработал scope=team в рекомендациях.
func (r *TeamRepo) SetManagerForMembers(ctx context.Context, teamID, managerEmpID uuid.UUID) error {
	_, err := r.pool.Exec(ctx, `
		UPDATE employees
		SET manager_id = $2
		WHERE id IN (SELECT employee_id FROM team_members WHERE team_id = $1)
		  AND id <> $2
	`, teamID, managerEmpID)
	return err
}

// itoa — мини-helper, чтобы не тянуть strconv ради одного места.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var buf [4]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	return string(buf[i:])
}

func scanTeam(s rowScanner) (*domain.Team, error) {
	var t domain.Team
	if err := s.Scan(&t.ID, &t.Name, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt); err != nil {
		return nil, err
	}
	return &t, nil
}

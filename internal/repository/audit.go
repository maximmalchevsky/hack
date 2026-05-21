package repository

import (
	"context"
	"encoding/json"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

// AuditRepo — журнал изменений. Кто, что, когда, с каких значений на какие.
type AuditRepo struct {
	pool *pgxpool.Pool
}

func NewAuditRepo(pool *pgxpool.Pool) *AuditRepo { return &AuditRepo{pool: pool} }

type AuditEntry struct {
	ActorUserID *uuid.UUID
	Action      string // create | update | delete | apply | dismiss
	Entity      string // work_profile | exception | integration | user | recommendation
	EntityID    *uuid.UUID
	Before      any
	After       any
	IP          string
	UserAgent   string
}

func (r *AuditRepo) Log(ctx context.Context, e AuditEntry) error {
	beforeJSON, _ := json.Marshal(e.Before)
	afterJSON, _ := json.Marshal(e.After)

	_, err := r.pool.Exec(ctx, `
		INSERT INTO audit_log (actor_user_id, action, entity, entity_id, before, after, ip, user_agent)
		VALUES ($1, $2, $3, $4,
		        CASE WHEN $5::text = 'null' OR $5::text = '' THEN NULL ELSE $5::jsonb END,
		        CASE WHEN $6::text = 'null' OR $6::text = '' THEN NULL ELSE $6::jsonb END,
		        NULLIF($7, '')::inet, NULLIF($8, ''))
	`, e.ActorUserID, e.Action, e.Entity, e.EntityID,
		string(beforeJSON), string(afterJSON), e.IP, e.UserAgent)
	return err
}

type AuditListFilter struct {
	Entity   string     // optional
	EntityID *uuid.UUID // optional
	Limit    int
}

type AuditRecord struct {
	ID          uuid.UUID  `json:"id"`
	ActorUserID *uuid.UUID `json:"actor_user_id,omitempty"`
	Action      string     `json:"action"`
	Entity      string     `json:"entity"`
	EntityID    *uuid.UUID `json:"entity_id,omitempty"`
	Before      any        `json:"before,omitempty"`
	After       any        `json:"after,omitempty"`
	CreatedAt   string     `json:"created_at"`
}

func (r *AuditRepo) List(ctx context.Context, f AuditListFilter) ([]AuditRecord, error) {
	if f.Limit <= 0 || f.Limit > 500 {
		f.Limit = 100
	}
	rows, err := r.pool.Query(ctx, `
		SELECT id, actor_user_id, action, entity, entity_id,
		       COALESCE(before::text, ''), COALESCE(after::text, ''),
		       created_at::text
		FROM audit_log
		WHERE ($1::text = '' OR entity = $1)
		  AND ($2::uuid IS NULL OR entity_id = $2)
		ORDER BY created_at DESC
		LIMIT $3
	`, f.Entity, nullUUIDArg(f.EntityID), f.Limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []AuditRecord
	for rows.Next() {
		var (
			rec        AuditRecord
			beforeStr  string
			afterStr   string
		)
		if err := rows.Scan(
			&rec.ID, &rec.ActorUserID, &rec.Action, &rec.Entity, &rec.EntityID,
			&beforeStr, &afterStr, &rec.CreatedAt,
		); err != nil {
			return nil, err
		}
		if beforeStr != "" {
			_ = json.Unmarshal([]byte(beforeStr), &rec.Before)
		}
		if afterStr != "" {
			_ = json.Unmarshal([]byte(afterStr), &rec.After)
		}
		out = append(out, rec)
	}
	return out, rows.Err()
}

func nullUUIDArg(id *uuid.UUID) any {
	if id == nil {
		return nil
	}
	return *id
}

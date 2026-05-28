package service

import (
	"context"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/rs/zerolog"

	"worktimesync/internal/repository"
)

type AuditService struct {
	repo *repository.AuditRepo
	log  zerolog.Logger
}

func NewAuditService(pool *pgxpool.Pool, log zerolog.Logger) *AuditService {
	return &AuditService{
		repo: repository.NewAuditRepo(pool),
		log:  log,
	}
}

type LogInput struct {
	ActorUserID *uuid.UUID
	Action      string
	Entity      string
	EntityID    *uuid.UUID
	Before      any
	After       any
	IP          string
	UserAgent   string
}

func (s *AuditService) Log(ctx context.Context, in LogInput) {
	err := s.repo.Log(ctx, repository.AuditEntry{
		ActorUserID: in.ActorUserID,
		Action:      in.Action,
		Entity:      in.Entity,
		EntityID:    in.EntityID,
		Before:      in.Before,
		After:       in.After,
		IP:          in.IP,
		UserAgent:   in.UserAgent,
	})
	if err != nil {
		s.log.Warn().Err(err).
			Str("action", in.Action).
			Str("entity", in.Entity).
			Msg("audit log failed")
	}
}

func (s *AuditService) List(ctx context.Context, entity string, entityID *uuid.UUID, limit int) ([]repository.AuditRecord, error) {
	return s.repo.List(ctx, repository.AuditListFilter{
		Entity:   entity,
		EntityID: entityID,
		Limit:    limit,
	})
}

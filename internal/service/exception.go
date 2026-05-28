package service

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

var (
	ErrInvalidException = errors.New("exception: invalid kind")
	ErrInvalidRange     = errors.New("exception: end_at must be after start_at")
)

type ExceptionService struct {
	repo *repository.ExceptionRepo
}

func NewExceptionService(pool *pgxpool.Pool) *ExceptionService {
	return &ExceptionService{repo: repository.NewExceptionRepo(pool)}
}

type CreateExceptionInput struct {
	EmployeeID uuid.UUID
	Kind       domain.ExceptionKind
	StartAt    time.Time
	EndAt      time.Time
	Comment    string
}

func (s *ExceptionService) Create(ctx context.Context, in CreateExceptionInput) (*domain.TimeException, error) {
	if !in.Kind.Valid() {
		return nil, ErrInvalidException
	}
	if !in.EndAt.After(in.StartAt) {
		return nil, ErrInvalidRange
	}
	return s.repo.Create(ctx, repository.CreateExceptionInput{
		EmployeeID: in.EmployeeID,
		Kind:       in.Kind,
		StartAt:    in.StartAt,
		EndAt:      in.EndAt,
		Comment:    in.Comment,
		Source:     "manual",
	})
}

func (s *ExceptionService) List(ctx context.Context, employeeID uuid.UUID, from, to time.Time) ([]domain.TimeException, error) {
	return s.repo.List(ctx, repository.ListExceptionsFilter{
		EmployeeID: employeeID,
		From:       from,
		To:         to,
	})
}

func (s *ExceptionService) Delete(ctx context.Context, id, employeeID uuid.UUID) error {
	return s.repo.Delete(ctx, id, employeeID)
}

func (s *ExceptionService) ByID(ctx context.Context, id uuid.UUID) (*domain.TimeException, error) {
	return s.repo.ByID(ctx, id)
}

package service

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/repository"
)

var (
	ErrInvalidWorkFormat = errors.New("profile: invalid work_format")
	ErrInvalidTimeRange  = errors.New("profile: end must be after start")
	ErrInvalidHours      = errors.New("profile: invalid hours format")
	ErrInvalidTimezone   = errors.New("profile: invalid timezone")
)

type ProfileService struct {
	pool  *pgxpool.Pool
	repo  *repository.WorkProfileRepo
	exc   *repository.ExceptionRepo
	audit *AuditService
}

func NewProfileService(pool *pgxpool.Pool, audit *AuditService) *ProfileService {
	return &ProfileService{
		pool:  pool,
		repo:  repository.NewWorkProfileRepo(pool),
		exc:   repository.NewExceptionRepo(pool),
		audit: audit,
	}
}

type UpdateProfileInput struct {
	EmployeeID uuid.UUID
	DaysOfWeek domain.DaysOfWeek
	Timezone   string
	WorkFormat domain.WorkFormat
}

var hhmmRE = regexp.MustCompile(`^\d{2}:\d{2}$`)

func validateDayHours(d *domain.DayHours) error {
	if d == nil {
		return nil
	}
	if !hhmmRE.MatchString(d.Start) || !hhmmRE.MatchString(d.End) {
		return ErrInvalidHours
	}
	s, err := time.Parse("15:04", d.Start)
	if err != nil {
		return ErrInvalidHours
	}
	e, err := time.Parse("15:04", d.End)
	if err != nil {
		return ErrInvalidHours
	}
	if !e.After(s) {
		return ErrInvalidTimeRange
	}
	return nil
}

func validateDaysOfWeek(d domain.DaysOfWeek) error {
	for _, dh := range []*domain.DayHours{d.Mon, d.Tue, d.Wed, d.Thu, d.Fri, d.Sat, d.Sun} {
		if err := validateDayHours(dh); err != nil {
			return err
		}
	}
	return nil
}

func (s *ProfileService) UpdateProfile(ctx context.Context, in UpdateProfileInput, actorUserID *uuid.UUID) (*domain.WorkProfile, error) {
	if !in.WorkFormat.Valid() {
		return nil, ErrInvalidWorkFormat
	}
	if err := validateDaysOfWeek(in.DaysOfWeek); err != nil {
		return nil, err
	}
	if in.Timezone == "" {
		in.Timezone = "Europe/Moscow"
	}
	if _, err := time.LoadLocation(in.Timezone); err != nil {
		return nil, ErrInvalidTimezone
	}

	prev, _ := s.repo.Active(ctx, in.EmployeeID)

	wp, err := s.repo.CreateNewVersion(ctx, repository.CreateProfileInput{
		EmployeeID: in.EmployeeID,
		DaysOfWeek: in.DaysOfWeek,
		Timezone:   in.Timezone,
		WorkFormat: in.WorkFormat,
		Source:     "manual",
	})
	if err != nil {
		return nil, err
	}

	if s.audit != nil {
		empID := in.EmployeeID
		s.audit.Log(ctx, LogInput{
			ActorUserID: actorUserID,
			Action:      "update",
			Entity:      "work_profile",
			EntityID:    &empID,
			Before:      prev,
			After:       wp,
		})
	}
	return wp, nil
}

func (s *ProfileService) Active(ctx context.Context, employeeID uuid.UUID) (*domain.WorkProfile, error) {
	return s.repo.Active(ctx, employeeID)
}

func (s *ProfileService) History(ctx context.Context, employeeID uuid.UUID) ([]domain.WorkProfile, error) {
	return s.repo.History(ctx, employeeID, 50)
}

func (s *ProfileService) ConfirmActive(ctx context.Context, employeeID uuid.UUID) error {
	tag, err := s.pool.Exec(ctx, `
		UPDATE employees SET last_confirmed_at = now() WHERE id = $1
	`, employeeID)
	if err != nil {
		return fmt.Errorf("confirm active: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return repository.ErrNotFound
	}
	return nil
}

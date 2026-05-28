package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/integrations"
	"worktimesync/internal/integrations/caldav"
	"worktimesync/internal/integrations/ical"
	"worktimesync/internal/integrations/jira"
	"worktimesync/internal/repository"
	"worktimesync/pkg/crypto"
)

var (
	ErrIntegrationBadInput = errors.New("integration: bad input")
	ErrIntegrationNotFound = errors.New("integration: not found")
)

type IntegrationService struct {
	pool     *pgxpool.Pool
	repo     *repository.IntegrationRepo
	cipher   *crypto.Cipher
	registry *integrations.Registry
}

func NewIntegrationService(pool *pgxpool.Pool, cipher *crypto.Cipher, registry *integrations.Registry) *IntegrationService {
	return &IntegrationService{
		pool:     pool,
		repo:     repository.NewIntegrationRepo(pool),
		cipher:   cipher,
		registry: registry,
	}
}

type ConnectICalInput struct {
	EmployeeID uuid.UUID
	FeedURL    string
	Label      string
}

func (s *IntegrationService) ConnectICal(ctx context.Context, in ConnectICalInput) (*domain.Integration, error) {
	prov := ical.New()
	authCode := in.FeedURL
	if authCode == "" {
		authCode = "manual"
	}
	tok, err := prov.Authenticate(ctx, authCode)
	if err != nil {
		return nil, fmt.Errorf("ical authenticate: %w", err)
	}

	enc, err := s.cipher.Encrypt(tok.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	cfg, _ := json.Marshal(map[string]any{
		"token_type": tok.TokenType,
	})

	return s.repo.Create(ctx, repository.CreateIntegrationInput{
		EmployeeID:     in.EmployeeID,
		Provider:       domain.IntegrationICal,
		AccountLabel:   in.Label,
		AccessTokenEnc: enc,
		ConfigJSON:     cfg,
	})
}

type ConnectCalDAVInput struct {
	EmployeeID uuid.UUID
	Endpoint   string
	Username   string
	Password   string
	CalPath    string
	Label      string
}

func (s *IntegrationService) ConnectCalDAV(ctx context.Context, in ConnectCalDAVInput) (*domain.Integration, error) {
	if in.Endpoint == "" || in.Username == "" || in.Password == "" {
		return nil, ErrIntegrationBadInput
	}

	payload, _ := json.Marshal(caldav.AuthPayload{
		Endpoint: in.Endpoint,
		Username: in.Username,
		Password: in.Password,
		CalPath:  in.CalPath,
	})

	prov := caldav.New()
	tok, err := prov.Authenticate(ctx, string(payload))
	if err != nil {
		return nil, fmt.Errorf("caldav authenticate: %w", err)
	}

	rawPayload, _ := tok.Raw["payload"].(string)
	if rawPayload == "" {
		rawPayload = string(payload)
	}
	enc, err := s.cipher.Encrypt(rawPayload)
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	cfg, _ := json.Marshal(map[string]any{
		"endpoint": in.Endpoint,
		"username": in.Username,
		"cal_path": tok.Raw["cal_path"],
	})

	return s.repo.Create(ctx, repository.CreateIntegrationInput{
		EmployeeID:     in.EmployeeID,
		Provider:       domain.IntegrationCalDAV,
		AccountEmail:   in.Username,
		AccountLabel:   in.Label,
		AccessTokenEnc: enc,
		ConfigJSON:     cfg,
	})
}

type ConnectYandexInput struct {
	EmployeeID   uuid.UUID
	AccessToken  string
	RefreshToken string
	ExpiresAt    time.Time
	AccountEmail string
	Label        string
}

func (s *IntegrationService) ConnectYandexCalendar(ctx context.Context, in ConnectYandexInput) (*domain.Integration, error) {
	if in.AccessToken == "" {
		return nil, ErrIntegrationBadInput
	}

	accEnc, err := s.cipher.Encrypt(in.AccessToken)
	if err != nil {
		return nil, fmt.Errorf("encrypt access: %w", err)
	}
	var refEnc string
	if in.RefreshToken != "" {
		refEnc, err = s.cipher.Encrypt(in.RefreshToken)
		if err != nil {
			return nil, fmt.Errorf("encrypt refresh: %w", err)
		}
	}

	cfg, _ := json.Marshal(map[string]any{
		"oauth_provider": "yandex",
		"endpoint":       "https://caldav.yandex.ru",
	})

	var expPtr *time.Time
	if !in.ExpiresAt.IsZero() {
		t := in.ExpiresAt
		expPtr = &t
	}

	return s.repo.Create(ctx, repository.CreateIntegrationInput{
		EmployeeID:      in.EmployeeID,
		Provider:        domain.IntegrationYandexCalendar,
		AccountEmail:    in.AccountEmail,
		AccountLabel:    in.Label,
		AccessTokenEnc:  accEnc,
		RefreshTokenEnc: refEnc,
		ExpiresAt:       expPtr,
		ConfigJSON:      cfg,
	})
}

type ConnectJiraInput struct {
	EmployeeID uuid.UUID
	BaseURL    string
	Email      string
	APIToken   string
	Label      string
}

func (s *IntegrationService) ConnectJira(ctx context.Context, in ConnectJiraInput) (*domain.Integration, error) {
	if in.BaseURL == "" || in.Email == "" || in.APIToken == "" {
		return nil, ErrIntegrationBadInput
	}

	payload, _ := json.Marshal(jira.AuthPayload{
		BaseURL:  in.BaseURL,
		Email:    in.Email,
		APIToken: in.APIToken,
	})

	prov := jira.New()
	if _, err := prov.Authenticate(ctx, string(payload)); err != nil {
		return nil, fmt.Errorf("jira authenticate: %w", err)
	}

	enc, err := s.cipher.Encrypt(string(payload))
	if err != nil {
		return nil, fmt.Errorf("encrypt: %w", err)
	}

	cfg, _ := json.Marshal(map[string]any{
		"base_url": in.BaseURL,
		"email":    in.Email,
	})

	label := in.Label
	if label == "" {
		label = "Jira"
	}

	return s.repo.Create(ctx, repository.CreateIntegrationInput{
		EmployeeID:     in.EmployeeID,
		Provider:       domain.IntegrationJira,
		AccountEmail:   in.Email,
		AccountLabel:   label,
		AccessTokenEnc: enc,
		ConfigJSON:     cfg,
	})
}

func (s *IntegrationService) ListByEmployee(ctx context.Context, employeeID uuid.UUID) ([]domain.Integration, error) {
	return s.repo.ListByEmployee(ctx, employeeID)
}

func (s *IntegrationService) Delete(ctx context.Context, id, employeeID uuid.UUID) error {
	err := s.repo.Delete(ctx, id, employeeID)
	if errors.Is(err, repository.ErrNotFound) {
		return ErrIntegrationNotFound
	}
	return err
}

func (s *IntegrationService) ByID(ctx context.Context, id uuid.UUID) (*domain.Integration, error) {
	i, err := s.repo.ByID(ctx, id)
	if errors.Is(err, repository.ErrNotFound) {
		return nil, ErrIntegrationNotFound
	}
	return i, err
}

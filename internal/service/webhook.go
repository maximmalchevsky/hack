package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/integrations"
	"worktimesync/internal/repository"
)

type SyncEnqueuer interface {
	EnqueueSyncIncremental(integrationID uuid.UUID) error
}

type WebhookService struct {
	pool         *pgxpool.Pool
	integrations *repository.IntegrationRepo
	registry     *integrations.Registry
	enqueuer     SyncEnqueuer
}

func NewWebhookService(pool *pgxpool.Pool, registry *integrations.Registry, enq SyncEnqueuer) *WebhookService {
	return &WebhookService{
		pool:         pool,
		integrations: repository.NewIntegrationRepo(pool),
		registry:     registry,
		enqueuer:     enq,
	}
}

type HandleResult struct {
	InboxID            uuid.UUID
	Provider           domain.IntegrationProvider
	ValidationResponse string
}

func (s *WebhookService) Handle(ctx context.Context, provider domain.IntegrationProvider, r *http.Request) (*HandleResult, error) {
	if !provider.Valid() {
		return nil, fmt.Errorf("webhook: unknown provider %q", provider)
	}

	if v := r.URL.Query().Get("validationToken"); v != "" {
		return &HandleResult{
			Provider:           provider,
			ValidationResponse: v,
		}, nil
	}

	body, err := io.ReadAll(r.Body)
	if err != nil {
		return nil, fmt.Errorf("webhook: read body: %w", err)
	}

	var asMap map[string]any
	_ = json.Unmarshal(body, &asMap)
	rawJSON := body
	if len(rawJSON) == 0 {
		rawJSON = []byte("{}")
	}

	inboxID, signatureOK := s.persistInbox(ctx, provider, rawJSON, true)

	calProv, _ := s.registry.Calendar(integrations.Provider(provider))
	if calProv != nil {
		_ = signatureOK
	}

	if s.enqueuer != nil {
		active, err := s.integrations.ListActive(ctx)
		if err == nil {
			for _, intg := range active {
				if intg.Provider == provider {
					_ = s.enqueuer.EnqueueSyncIncremental(intg.ID)
				}
			}
		}
	}

	return &HandleResult{
		InboxID:  inboxID,
		Provider: provider,
	}, nil
}

func (s *WebhookService) persistInbox(ctx context.Context, provider domain.IntegrationProvider, rawJSON []byte, signatureOK bool) (uuid.UUID, bool) {
	var id uuid.UUID
	_ = s.pool.QueryRow(ctx, `
		INSERT INTO webhook_inbox (provider, signature_ok, payload)
		VALUES ($1, $2, $3::jsonb)
		RETURNING id
	`, string(provider), signatureOK, string(rawJSON)).Scan(&id)
	return id, signatureOK
}

func (s *WebhookService) MarkProcessed(ctx context.Context, inboxID uuid.UUID, errMsg string) error {
	if errMsg != "" {
		_, err := s.pool.Exec(ctx, `
			UPDATE webhook_inbox SET processed_at = now(), error = $2 WHERE id = $1
		`, inboxID, errMsg)
		return err
	}
	_, err := s.pool.Exec(ctx, `
		UPDATE webhook_inbox SET processed_at = now() WHERE id = $1
	`, inboxID)
	return err
}

var ErrUnsupportedProvider = errors.New("webhook: provider not supported")

var _ = time.Second

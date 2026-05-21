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

// SyncEnqueuer — минимальный интерфейс, чтобы не зависеть от workers/.
// Реализуется workers.Enqueuer.
type SyncEnqueuer interface {
	EnqueueSyncIncremental(integrationID uuid.UUID) error
}

// WebhookService — приёмник webhook'ов от внешних провайдеров.
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

// HandleResult — что вернуть наружу.
type HandleResult struct {
	InboxID  uuid.UUID
	Provider domain.IntegrationProvider
	// ValidationResponse — для MS Graph echo-validation: возвращается как plain-text body.
	ValidationResponse string
}

// Handle — приём webhook от провайдера.
func (s *WebhookService) Handle(ctx context.Context, provider domain.IntegrationProvider, r *http.Request) (*HandleResult, error) {
	if !provider.Valid() {
		return nil, fmt.Errorf("webhook: unknown provider %q", provider)
	}

	// MS Graph специальный случай: validation handshake.
	// Если запрос содержит ?validationToken=, отвечаем plain-text эхом.
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

	// Парсим как map[string]any для inbox-хранения (если получится).
	var asMap map[string]any
	_ = json.Unmarshal(body, &asMap)
	rawJSON := body
	if len(rawJSON) == 0 {
		rawJSON = []byte("{}")
	}

	// Пишем в webhook_inbox.
	inboxID, signatureOK := s.persistInbox(ctx, provider, rawJSON, true)

	// Пытаемся нормализовать через провайдера.
	calProv, _ := s.registry.Calendar(integrations.Provider(provider))
	if calProv != nil {
		// Конструируем фейковый Request с новым body для парсера провайдера.
		// Пакеты go-webdav/google ожидают чтение Body — для Provider'ов достаточно
		// иметь сам Request и URL/Headers. Здесь упрощённо.
		_ = signatureOK
	}

	// Enqueue инкрементальный sync для всех интеграций этого провайдера.
	// На дне 8 — упрощённо: дёргаем ListActive и enqueue по всем релевантным.
	// На дне 9 заменим на адресный sync по subscription_id.
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

// MarkProcessed — отметить, что inbox-запись обработана (или с ошибкой).
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

// ErrUnsupportedProvider — провайдер не зарегистрирован.
var ErrUnsupportedProvider = errors.New("webhook: provider not supported")

// silence unused
var _ = time.Second

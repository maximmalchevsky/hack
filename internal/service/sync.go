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
	"worktimesync/internal/integrations/yandex"
	"worktimesync/internal/repository"
	"worktimesync/pkg/crypto"
)

// SyncService — выполняет инкрементальный sync одной интеграции.
type SyncService struct {
	pool         *pgxpool.Pool
	integrations *repository.IntegrationRepo
	events       *repository.CalendarEventRepo
	cipher       *crypto.Cipher
	yandex       *yandex.Provider // nil если OAuth Яндекса не настроен
}

func NewSyncService(pool *pgxpool.Pool, cipher *crypto.Cipher) *SyncService {
	return &SyncService{
		pool:         pool,
		integrations: repository.NewIntegrationRepo(pool),
		events:       repository.NewCalendarEventRepo(pool),
		cipher:       cipher,
	}
}

// WithYandex — DI: подключаем Yandex Calendar provider (если в .env есть креды).
func (s *SyncService) WithYandex(p *yandex.Provider) *SyncService {
	s.yandex = p
	return s
}

// SyncResult — итог одной синхронизации.
type SyncResult struct {
	IntegrationID uuid.UUID
	EmployeeID    uuid.UUID
	Provider      domain.IntegrationProvider
	EventsLoaded  int
	From          time.Time
	To            time.Time
}

// SyncIntegration — загружает события за окно [-30d..+60d] и пишет в БД.
// При ошибке статус интеграции переводится в error.
func (s *SyncService) SyncIntegration(ctx context.Context, integrationID uuid.UUID) (*SyncResult, error) {
	integ, err := s.integrations.ByID(ctx, integrationID)
	if err != nil {
		return nil, fmt.Errorf("sync: load integration: %w", err)
	}

	from := time.Now().UTC().AddDate(0, 0, -30)
	to := time.Now().UTC().AddDate(0, 0, 60)

	events, err := s.fetchEvents(ctx, integ, from, to)
	if err != nil {
		_ = s.integrations.MarkSyncError(ctx, integrationID, err.Error())
		return nil, err
	}

	loaded := 0
	for _, ev := range events {
		_, upErr := s.events.Upsert(ctx, repository.UpsertEventInput{
			EmployeeID:     integ.EmployeeID,
			IntegrationID:  &integ.ID,
			SourceEventID:  ev.SourceID,
			Title:          ev.Title,
			Description:    ev.Description,
			StartAt:        ev.StartAt,
			EndAt:          ev.EndAt,
			Timezone:       ev.Timezone,
			IsRecurring:    ev.IsRecurring,
			RRule:          ev.RRule,
			Organizer:      ev.Organizer,
			AttendeesCount: ev.AttendeesCount,
			Status:         mapStatus(ev.Status),
		})
		if upErr != nil {
			// не валим всю синхру — пропускаем одно событие, копим ошибки в last_error
			continue
		}
		loaded++
	}

	if err := s.integrations.MarkSyncSuccess(ctx, integrationID); err != nil {
		return nil, err
	}
	return &SyncResult{
		IntegrationID: integrationID,
		EmployeeID:    integ.EmployeeID,
		Provider:      integ.Provider,
		EventsLoaded:  loaded,
		From:          from,
		To:            to,
	}, nil
}

func (s *SyncService) fetchEvents(ctx context.Context, integ *domain.Integration, from, to time.Time) ([]integrations.Event, error) {
	switch integ.Provider {
	case domain.IntegrationICal:
		return s.fetchICal(ctx, integ, from, to)
	case domain.IntegrationCalDAV:
		return s.fetchCalDAV(ctx, integ, from, to)
	case domain.IntegrationYandexCalendar:
		return s.fetchYandex(ctx, integ, from, to)
	default:
		return nil, fmt.Errorf("sync: provider %q not implemented yet", integ.Provider)
	}
}

func (s *SyncService) fetchYandex(ctx context.Context, integ *domain.Integration, from, to time.Time) ([]integrations.Event, error) {
	if s.yandex == nil {
		return nil, errors.New("sync: yandex provider not configured (set OAUTH_YANDEX_CLIENT_ID/SECRET)")
	}
	access, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("yandex sync: decrypt access: %w", err)
	}
	refresh := ""
	if integ.RefreshTokenEnc != "" {
		if r, derr := s.cipher.Decrypt(integ.RefreshTokenEnc); derr == nil {
			refresh = r
		}
	}

	tok := &integrations.Token{
		AccessToken:  access,
		RefreshToken: refresh,
		TokenType:    "OAuth",
		Raw:          map[string]any{},
	}
	if integ.ExpiresAt != nil {
		tok.Expiry = *integ.ExpiresAt
	}

	// Если токен истёк — обновляем через refresh.
	if !tok.Expiry.IsZero() && time.Until(tok.Expiry) < time.Minute && refresh != "" {
		newTok, rerr := s.yandex.RefreshToken(ctx, tok)
		if rerr == nil && newTok != nil {
			tok = newTok
			// Сохраняем обновлённые токены (зашифрованно).
			if enc, eerr := s.cipher.Encrypt(tok.AccessToken); eerr == nil {
				_ = s.integrations.UpdateTokens(ctx, integ.ID, enc,
					encryptOrEmpty(s.cipher, tok.RefreshToken), tok.Expiry)
			}
		}
	}

	return s.yandex.FetchEvents(ctx, tok, from, to)
}

func encryptOrEmpty(c *crypto.Cipher, s string) string {
	if s == "" {
		return ""
	}
	enc, err := c.Encrypt(s)
	if err != nil {
		return ""
	}
	return enc
}

func (s *SyncService) fetchICal(ctx context.Context, integ *domain.Integration, from, to time.Time) ([]integrations.Event, error) {
	tokStr, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}
	if tokStr == "" {
		// manual upload — нет URL, синк нечего тянуть
		return nil, nil
	}
	prov := ical.New()
	tok := &integrations.Token{AccessToken: tokStr, TokenType: "feed"}
	return prov.FetchEvents(ctx, tok, from, to)
}

func (s *SyncService) fetchCalDAV(ctx context.Context, integ *domain.Integration, from, to time.Time) ([]integrations.Event, error) {
	payloadStr, err := s.cipher.Decrypt(integ.AccessTokenEnc)
	if err != nil {
		return nil, fmt.Errorf("decrypt token: %w", err)
	}
	if payloadStr == "" {
		return nil, errors.New("sync: empty caldav payload")
	}

	// извлекаем cal_path из config или из payload
	var cfg map[string]any
	if len(integ.ConfigJSON) > 0 {
		_ = json.Unmarshal(integ.ConfigJSON, &cfg)
	}
	calPath := ""
	if cfg != nil {
		if cp, ok := cfg["cal_path"].(string); ok {
			calPath = cp
		}
	}

	prov := caldav.New()
	tok := &integrations.Token{
		TokenType: "basic",
		Raw: map[string]any{
			"payload":  payloadStr,
			"cal_path": calPath,
		},
	}
	return prov.FetchEvents(ctx, tok, from, to)
}

func mapStatus(s string) domain.EventStatus {
	switch s {
	case "tentative":
		return domain.EventTentative
	case "cancelled":
		return domain.EventCancelled
	default:
		return domain.EventConfirmed
	}
}

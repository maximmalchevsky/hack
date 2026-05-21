package oauth

import (
	"context"
	"errors"
	"fmt"

	"worktimesync/internal/config"
	"worktimesync/internal/integrations"
)

// Hub — единая точка OAuth-flow для всех провайдеров, поддерживающих OAuth2.
// Не-OAuth провайдеры (iCal feed, CalDAV/Basic) сюда не попадают.
//
// В спринте 1 — заготовка. В спринте 2 реализуем Google/MS/Yandex по факту
// готовности приложений (см. план §11 spring 2 day 8).
type Hub struct {
	cfg      *config.OAuth
	registry *integrations.Registry
}

func New(cfg *config.OAuth, registry *integrations.Registry) *Hub {
	return &Hub{cfg: cfg, registry: registry}
}

// AuthorizeURL возвращает URL, на который надо отправить пользователя
// для начала OAuth-flow.
func (h *Hub) AuthorizeURL(ctx context.Context, provider integrations.Provider, state string) (string, error) {
	switch provider {
	case integrations.ProviderGoogleCalendar:
		if h.cfg.GoogleClientID == "" {
			return "", errors.New("google oauth not configured")
		}
		return "", errors.New("google oauth: not implemented yet (sprint 2)")
	case integrations.ProviderMicrosoft365:
		if h.cfg.MicrosoftClientID == "" {
			return "", errors.New("microsoft oauth not configured")
		}
		return "", errors.New("microsoft oauth: not implemented yet (sprint 2)")
	case integrations.ProviderYandexTracker:
		if h.cfg.YandexClientID == "" {
			return "", errors.New("yandex oauth not configured")
		}
		return "", errors.New("yandex oauth: not implemented yet (sprint 2)")
	default:
		return "", fmt.Errorf("provider %q does not use oauth", provider)
	}
}

// ExchangeCode обменивает authorization_code на Token. Делегирует к
// конкретному провайдеру через Registry.
func (h *Hub) ExchangeCode(ctx context.Context, provider integrations.Provider, code string) (*integrations.Token, error) {
	cal, ok := h.registry.Calendar(provider)
	if ok {
		return cal.Authenticate(ctx, code)
	}
	tr, ok := h.registry.Tracker(provider)
	if ok {
		return tr.Authenticate(ctx, code)
	}
	return nil, integrations.ErrProviderNotRegistered
}

package oauth

import (
	"context"
	"errors"
	"fmt"

	"worktimesync/internal/config"
	"worktimesync/internal/integrations"
)

type Hub struct {
	cfg      *config.OAuth
	registry *integrations.Registry
}

func New(cfg *config.OAuth, registry *integrations.Registry) *Hub {
	return &Hub{cfg: cfg, registry: registry}
}

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

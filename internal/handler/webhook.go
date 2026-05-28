package handler

import (
	"bytes"
	"io"
	"net/http"
	"net/url"

	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/domain"
	"worktimesync/internal/service"
)

type WebhookHandler struct {
	svc *service.WebhookService
}

func NewWebhookHandler(svc *service.WebhookService) *WebhookHandler {
	return &WebhookHandler{svc: svc}
}

func (h *WebhookHandler) Mount(r fiber.Router) {
	r.All("/webhooks/:provider", h.handle)
}

func (h *WebhookHandler) handle(c fiber.Ctx) error {
	provider := domain.IntegrationProvider(c.Params("provider"))
	if !provider.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "unknown provider")
	}

	res, err := h.svc.Handle(c.Context(), provider, buildStdRequest(c))
	if err != nil {
		return err
	}

	if res.ValidationResponse != "" {
		c.Set("Content-Type", "text/plain")
		return c.SendString(res.ValidationResponse)
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"received": true,
		"inbox_id": res.InboxID,
		"provider": res.Provider,
	})
}

func buildStdRequest(c fiber.Ctx) *http.Request {
	body := c.Body()
	rawURL := string(c.Request().URI().FullURI())
	parsedURL, err := url.Parse(rawURL)
	if err != nil {
		parsedURL = &url.URL{Path: c.Path(), RawQuery: string(c.Request().URI().QueryString())}
	}
	req := &http.Request{
		Method: c.Method(),
		URL:    parsedURL,
		Header: http.Header{},
		Body:   io.NopCloser(bytes.NewReader(body)),
	}
	c.Request().Header.VisitAll(func(k, v []byte) {
		req.Header.Add(string(k), string(v))
	})
	return req
}

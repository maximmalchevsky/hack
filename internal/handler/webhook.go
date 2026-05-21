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

// Mount монтирует /api/v1/webhooks/:provider.
// Webhook'и не требуют JWT-авторизации — провайдеры подписываются на стороне сервера,
// валидация подписи происходит внутри сервиса.
func (h *WebhookHandler) Mount(r fiber.Router) {
	r.All("/webhooks/:provider", h.handle)
}

func (h *WebhookHandler) handle(c fiber.Ctx) error {
	provider := domain.IntegrationProvider(c.Params("provider"))
	if !provider.Valid() {
		return fiber.NewError(fiber.StatusBadRequest, "unknown provider")
	}

	// Конвертируем Fiber-контекст в стандартный http.Request для сервиса.
	// Fiber v3 предоставляет c.Request() возвращающий fasthttp.Request,
	// для нашего сервиса достаточно URL+headers+body — соберём заглушку через
	// c.Request().URI().QueryArgs() и c.BodyRaw().
	res, err := h.svc.Handle(c.Context(), provider, buildStdRequest(c))
	if err != nil {
		return err
	}

	if res.ValidationResponse != "" {
		c.Set("Content-Type", "text/plain")
		return c.SendString(res.ValidationResponse)
	}

	return c.Status(fiber.StatusAccepted).JSON(fiber.Map{
		"received":  true,
		"inbox_id":  res.InboxID,
		"provider":  res.Provider,
	})
}

// buildStdRequest — конвертирует Fiber-контекст в стандартный *http.Request
// для нужд WebhookService (он использует только URL.Query и Body).
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

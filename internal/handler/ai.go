package handler

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type AIHandler struct {
	svc *service.AIChatService
}

func NewAIHandler(chat *service.AIChatService) *AIHandler {
	return &AIHandler{svc: chat}
}

func (h *AIHandler) Mount(r fiber.Router) {
	g := r.Group("/ai")
	g.Get("/status", h.status)
	g.Get("/health", h.health)
	g.Post("/chat", h.handleChat)
	g.Post("/chat/stream", h.handleChatStream)
	g.Get("/conversations/latest", h.latestConversation)
	g.Get("/conversations/:id/messages", h.conversationMessages)
	g.Delete("/conversations/:id", h.deleteConversation)
}

func (h *AIHandler) latestConversation(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	id, err := h.svc.LatestConversation(c.Context(), userID)
	if err != nil {
		return err
	}
	if id == uuid.Nil {
		return c.JSON(fiber.Map{"conversation_id": nil})
	}
	return c.JSON(fiber.Map{"conversation_id": id.String()})
}

func (h *AIHandler) conversationMessages(c fiber.Ctx) error {
	convID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid conversation id")
	}
	userID := middleware.UserID(c)
	list, err := h.svc.ListMessages(c.Context(), convID, userID)
	if err != nil {
		if errors.Is(err, service.ErrChatForbidden) {
			return fiber.NewError(fiber.StatusForbidden, "not your conversation")
		}
		return err
	}
	return c.JSON(fiber.Map{"messages": list})
}

func (h *AIHandler) deleteConversation(c fiber.Ctx) error {
	convID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid conversation id")
	}
	userID := middleware.UserID(c)
	if err := h.svc.DeleteConversation(c.Context(), convID, userID); err != nil {
		if errors.Is(err, service.ErrChatForbidden) {
			return fiber.NewError(fiber.StatusForbidden, "not your conversation")
		}
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

func (h *AIHandler) status(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"available": h.svc.ChatAvailable()})
}

func (h *AIHandler) health(c fiber.Ctx) error {
	if !h.svc.ChatAvailable() {
		return c.JSON(fiber.Map{
			"ok":     false,
			"reason": "llm_not_configured",
		})
	}
	t0 := time.Now()
	model, err := h.svc.HealthPing(c.Context())
	latency := time.Since(t0)
	if err != nil {
		return c.JSON(fiber.Map{
			"ok":         false,
			"reason":     err.Error(),
			"latency_ms": latency.Milliseconds(),
		})
	}
	return c.JSON(fiber.Map{
		"ok":         true,
		"model":      model,
		"latency_ms": latency.Milliseconds(),
	})
}

func (h *AIHandler) handleChat(c fiber.Ctx) error {
	var req AIChatRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Message == "" {
		return fiber.NewError(fiber.StatusBadRequest, "message required")
	}

	answer, convID, err := h.svc.Ask(c.Context(), middleware.UserID(c), req.ConversationID, req.Message)
	if err != nil {
		return err
	}
	return c.JSON(AIChatResponse{
		ConversationID: convID,
		Answer:         answer,
		Available:      h.svc.ChatAvailable(),
	})
}

func (h *AIHandler) handleChatStream(c fiber.Ctx) error {
	var req AIChatRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if req.Message == "" {
		return fiber.NewError(fiber.StatusBadRequest, "message required")
	}

	userID := middleware.UserID(c)
	ctx := c.Context()
	stream, err := h.svc.AskStream(ctx, userID, req.ConversationID, req.Message)
	if err != nil {
		return err
	}

	c.Set("Content-Type", "text/event-stream; charset=utf-8")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	return c.SendStreamWriter(func(w *bufio.Writer) {
		writeEvent := func(event string, payload any) bool {
			b, _ := json.Marshal(payload)
			if _, err := fmt.Fprintf(w, "event: %s\ndata: %s\n\n", event, b); err != nil {
				return false
			}
			return w.Flush() == nil
		}

		metaSent := false
		for ev := range stream {
			if !metaSent {
				if !writeEvent("meta", fiber.Map{"conversation_id": ev.ConversationID.String()}) {
					return
				}
				metaSent = true
			}
			if ev.Err != nil {
				if !writeEvent("error", fiber.Map{"message": ev.Err.Error()}) {
					return
				}
			}
			if ev.Delta != "" {
				if !writeEvent("delta", fiber.Map{"text": ev.Delta}) {
					return
				}
			}
			if ev.Done {
				_ = writeEvent("done", fiber.Map{})
				return
			}
		}
	})
}

package handler

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type NotificationHandler struct {
	svc   *service.NotificationService
	redis *redis.Client
}

func NewNotificationHandler(svc *service.NotificationService, rdb *redis.Client) *NotificationHandler {
	return &NotificationHandler{svc: svc, redis: rdb}
}

func (h *NotificationHandler) Mount(r fiber.Router) {
	g := r.Group("/notifications")
	g.Get("/", h.list)
	g.Get("/count", h.countUnread)
	g.Get("/stream", h.sse)
	g.Post("/broadcast", h.broadcast)
	g.Post("/:id/read", h.markRead)
	g.Post("/read-all", h.markAllRead)
}

type broadcastRequest struct {
	Kind        string      `json:"kind"`
	EmployeeIDs []uuid.UUID `json:"employee_ids"`
}

func (h *NotificationHandler) broadcast(c fiber.Ctx) error {
	role := middleware.CurrentRole(c)
	switch role {
	case domain.RoleManager, domain.RolePM, domain.RoleHR, domain.RoleAdmin:
	default:
		return fiber.NewError(fiber.StatusForbidden, "only manager/pm/hr/admin")
	}
	var req broadcastRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Kind == "" {
		return fiber.NewError(fiber.StatusBadRequest, "kind required")
	}
	if len(req.EmployeeIDs) == 0 {
		return c.JSON(fiber.Map{"sent": 0, "skipped": 0, "targeted": 0})
	}

	res, err := h.svc.NotifyByKind(c.Context(), req.Kind, req.EmployeeIDs, middleware.UserID(c))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(res)
}

func (h *NotificationHandler) list(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	onlyUnread := c.Query("unread") == "true"

	list, err := h.svc.List(c.Context(), userID, onlyUnread)
	if err != nil {
		return err
	}
	out := make([]NotificationDTO, 0, len(list))
	for _, n := range list {
		out = append(out, notificationToDTO(n))
	}
	return c.JSON(fiber.Map{"notifications": out})
}

func (h *NotificationHandler) countUnread(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	n, err := h.svc.CountUnread(c.Context(), userID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"unread": n})
}

func (h *NotificationHandler) markRead(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.svc.MarkRead(c.Context(), id, userID); err != nil {
		return err
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *NotificationHandler) markAllRead(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if err := h.svc.MarkAllRead(c.Context(), userID); err != nil {
		return err
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *NotificationHandler) sse(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "auth required")
	}
	if h.redis == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "redis unavailable")
	}

	c.Set("Content-Type", "text/event-stream")
	c.Set("Cache-Control", "no-cache")
	c.Set("Connection", "keep-alive")
	c.Set("X-Accel-Buffering", "no")

	channel := service.WSNotificationsChannel + userID.String()
	ctx, cancel := context.WithCancel(c.Context())
	defer cancel()

	pubsub := h.redis.Subscribe(ctx, channel)
	defer pubsub.Close()

	return c.SendStreamWriter(func(w *bufio.Writer) {
		_, _ = fmt.Fprintf(w, "event: ready\ndata: %s\n\n", `{"ok":true}`)
		_ = w.Flush()

		ch := pubsub.Channel()
		ticker := time.NewTicker(20 * time.Second)
		defer ticker.Stop()

		for {
			select {
			case <-ctx.Done():
				return
			case msg, ok := <-ch:
				if !ok {
					return
				}
				if _, err := fmt.Fprintf(w, "event: notification\ndata: %s\n\n", msg.Payload); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			case <-ticker.C:
				if _, err := fmt.Fprintf(w, ": keepalive\n\n"); err != nil {
					return
				}
				if err := w.Flush(); err != nil {
					return
				}
			}
		}
	})
}

type NotificationDTO struct {
	ID        uuid.UUID       `json:"id"`
	Kind      string          `json:"kind"`
	Title     string          `json:"title"`
	Body      string          `json:"body,omitempty"`
	Link      string          `json:"link,omitempty"`
	Read      bool            `json:"read"`
	CreatedAt time.Time       `json:"created_at"`
	Payload   json.RawMessage `json:"payload,omitempty"`
}

func notificationToDTO(n domain.Notification) NotificationDTO {
	d := NotificationDTO{
		ID:        n.ID,
		Kind:      n.Kind,
		Title:     n.Title,
		Body:      n.Body,
		Link:      n.Link,
		Read:      n.ReadAt != nil,
		CreatedAt: n.CreatedAt,
	}
	if len(n.Payload) > 0 {
		d.Payload = json.RawMessage(n.Payload)
	}
	return d
}

func safeJSON(v any) string {
	b, err := json.Marshal(v)
	if err != nil {
		return "{}"
	}
	return string(b)
}

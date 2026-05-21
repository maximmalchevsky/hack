package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type AuditHandler struct {
	svc *service.AuditService
}

func NewAuditHandler(svc *service.AuditService) *AuditHandler {
	return &AuditHandler{svc: svc}
}

func (h *AuditHandler) Mount(r fiber.Router) {
	g := r.Group("/admin/audit", middleware.RequireRole(domain.RoleAdmin))
	g.Get("/", h.list)
}

func (h *AuditHandler) list(c fiber.Ctx) error {
	entity := c.Query("entity")
	var entityID *uuid.UUID
	if v := c.Query("entity_id"); v != "" {
		if id, err := uuid.Parse(v); err == nil {
			entityID = &id
		}
	}
	limit := 100
	if v := c.Query("limit"); v != "" {
		if n, err := strconv.Atoi(v); err == nil && n > 0 && n <= 500 {
			limit = n
		}
	}

	list, err := h.svc.List(c.Context(), entity, entityID, limit)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"records": list})
}

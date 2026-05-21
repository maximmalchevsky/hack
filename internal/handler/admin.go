package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type AdminHandler struct {
	svc *service.AdminService
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// Mount монтирует /api/v1/admin/* с middleware RequireRole(admin).
func (h *AdminHandler) Mount(r fiber.Router) {
	g := r.Group("/admin", middleware.RequireRole(domain.RoleAdmin))
	g.Get("/users", h.listUsers)
	g.Patch("/users/:id/role", h.updateRole)
	g.Get("/sources", h.listSources)
	g.Get("/rules", h.getRules)
	g.Put("/rules", h.updateRules)
	g.Get("/system/health", h.health)
}

func (h *AdminHandler) listUsers(c fiber.Ctx) error {
	list, err := h.svc.ListUsers(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": list})
}

type UpdateRoleRequest struct {
	Role string `json:"role"`
}

func (h *AdminHandler) updateRole(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req UpdateRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.svc.UpdateRole(c.Context(), id, domain.Role(req.Role)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *AdminHandler) listSources(c fiber.Ctx) error {
	list, err := h.svc.ListIntegrations(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"sources": list})
}

func (h *AdminHandler) getRules(c fiber.Ctx) error {
	w, err := h.svc.GetWeights(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(w)
}

func (h *AdminHandler) updateRules(c fiber.Ctx) error {
	var w service.AnalyticsWeights
	if err := c.Bind().Body(&w); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.svc.UpdateWeights(c.Context(), w); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *AdminHandler) health(c fiber.Ctx) error {
	h2, err := h.svc.Health(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(h2)
}

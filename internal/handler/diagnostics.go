package handler

import (
	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/service"
)

type DiagnosticsHandler struct {
	svc       *service.DiagnosticsService
	conflicts *service.ConflictsService
}

func NewDiagnosticsHandler(svc *service.DiagnosticsService, conflicts *service.ConflictsService) *DiagnosticsHandler {
	return &DiagnosticsHandler{svc: svc, conflicts: conflicts}
}

func (h *DiagnosticsHandler) Mount(r fiber.Router) {
	r.Get("/diagnostics/groups", h.groups)
	r.Get("/diagnostics/burnout", h.burnout)
}

func (h *DiagnosticsHandler) groups(c fiber.Ctx) error {
	g, err := h.svc.Build(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(g)
}

func (h *DiagnosticsHandler) burnout(c fiber.Ctx) error {
	list, err := h.svc.Burnout(c.Context(), h.conflicts)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"burnout": list})
}

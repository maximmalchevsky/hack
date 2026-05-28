package handler

import (
	"encoding/json"
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type ReportPresetsHandler struct {
	svc *service.ReportPresetService
}

func NewReportPresetsHandler(svc *service.ReportPresetService) *ReportPresetsHandler {
	return &ReportPresetsHandler{svc: svc}
}

func (h *ReportPresetsHandler) Mount(r fiber.Router) {
	g := r.Group("/report-presets")
	g.Get("/", h.list)
	g.Post("/", h.create)
	g.Put("/:id", h.update)
	g.Delete("/:id", h.delete)
}

func (h *ReportPresetsHandler) list(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	res, err := h.svc.List(c.Context(), userID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"presets": res})
}

type presetRequest struct {
	Name    string                      `json:"name"`
	Kind    string                      `json:"kind"`
	Columns []string                    `json:"columns"`
	Filters service.ReportPresetFilters `json:"filters"`
}

func (h *ReportPresetsHandler) create(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var req presetRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	res, err := h.svc.Create(c.Context(), userID, service.ReportPreset{
		Name:    req.Name,
		Kind:    req.Kind,
		Columns: req.Columns,
		Filters: req.Filters,
	})
	if err != nil {
		return mapPresetErr(err)
	}
	return c.JSON(res)
}

func (h *ReportPresetsHandler) update(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req presetRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	res, err := h.svc.Update(c.Context(), id, userID, service.ReportPreset{
		Name:    req.Name,
		Kind:    req.Kind,
		Columns: req.Columns,
		Filters: req.Filters,
	})
	if err != nil {
		return mapPresetErr(err)
	}
	return c.JSON(res)
}

func (h *ReportPresetsHandler) delete(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.svc.Delete(c.Context(), id, userID); err != nil {
		return mapPresetErr(err)
	}
	return c.JSON(fiber.Map{"ok": true})
}

func mapPresetErr(err error) error {
	switch {
	case errors.Is(err, service.ErrPresetNotFound):
		return fiber.NewError(fiber.StatusNotFound, "preset not found")
	case errors.Is(err, service.ErrPresetForbidden):
		return fiber.NewError(fiber.StatusForbidden, "not yours")
	case errors.Is(err, service.ErrPresetInvalid):
		return fiber.NewError(fiber.StatusBadRequest, "invalid preset")
	default:
		return err
	}
}

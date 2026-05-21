package handler

import (
	"encoding/json"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type ViewPresetsHandler struct {
	svc *service.ViewPresetsService
}

func NewViewPresetsHandler(svc *service.ViewPresetsService) *ViewPresetsHandler {
	return &ViewPresetsHandler{svc: svc}
}

func (h *ViewPresetsHandler) Mount(r fiber.Router) {
	r.Get("/view-presets", h.list)
	r.Post("/view-presets", h.create)
	r.Delete("/view-presets/:id", h.del)
}

func (h *ViewPresetsHandler) list(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	page := c.Query("page")
	if page == "" {
		return fiber.NewError(fiber.StatusBadRequest, "page is required")
	}
	list, err := h.svc.List(c.Context(), userID, page)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(fiber.Map{"presets": list})
}

type createPresetReq struct {
	Page    string          `json:"page"`
	Name    string          `json:"name"`
	Filters json.RawMessage `json:"filters"`
}

func (h *ViewPresetsHandler) create(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var req createPresetReq
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	p, err := h.svc.Create(c.Context(), userID, req.Page, req.Name, req.Filters)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(p)
}

func (h *ViewPresetsHandler) del(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad id")
	}
	if err := h.svc.Delete(c.Context(), userID, id); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

package handler

import (
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/service"
)

type ConflictsHandler struct {
	svc *service.ConflictsService
}

func NewConflictsHandler(svc *service.ConflictsService) *ConflictsHandler {
	return &ConflictsHandler{svc: svc}
}

func (h *ConflictsHandler) Mount(r fiber.Router) {
	g := r.Group("/conflicts")
	g.Get("/", h.list)
	g.Get("/employee/:id", h.byEmployee)
}

func (h *ConflictsHandler) list(c fiber.Ctx) error {
	from := parseTimeQuery(c, "from")
	to := parseTimeQuery(c, "to")
	list, err := h.svc.ListAll(c.Context(), from, to, 200)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"conflicts": list})
}

func (h *ConflictsHandler) byEmployee(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	from := parseTimeQuery(c, "from")
	to := parseTimeQuery(c, "to")
	list, err := h.svc.ListByEmployee(c.Context(), id, from, to)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"conflicts": list})
}

var _ = time.Time{}

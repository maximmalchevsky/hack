package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/service"
)

type EmployeeHandler struct {
	svc *service.EmployeeService
}

func NewEmployeeHandler(svc *service.EmployeeService) *EmployeeHandler {
	return &EmployeeHandler{svc: svc}
}

func (h *EmployeeHandler) Mount(r fiber.Router) {
	g := r.Group("/employees")
	g.Get("/", h.list)
	g.Get("/:id", h.detail)
}

func (h *EmployeeHandler) list(c fiber.Ctx) error {
	list, err := h.svc.List(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"employees": list})
}

func (h *EmployeeHandler) detail(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	d, err := h.svc.Detail(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(d)
}

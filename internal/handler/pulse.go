package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type PulseHandler struct {
	svc *service.PulseService
}

func NewPulseHandler(svc *service.PulseService) *PulseHandler {
	return &PulseHandler{svc: svc}
}

func (h *PulseHandler) Mount(r fiber.Router) {
	r.Get("/pulse/me", h.me)
	r.Post("/pulse", h.submit)
	r.Get("/pulse/team", h.team)
}

type submitReq struct {
	Score   int    `json:"score"`
	Comment string `json:"comment"`
}

func (h *PulseHandler) me(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no employee")
	}
	s, err := h.svc.Me(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(s)
}

func (h *PulseHandler) submit(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no employee")
	}
	var req submitReq
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	entry, err := h.svc.Submit(c.Context(), empID, req.Score, req.Comment)
	if err != nil {
		if errors.Is(err, service.ErrInvalidScore) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return err
	}
	return c.JSON(entry)
}

// team — менеджер видит pulse-ответы своих сотрудников.
func (h *PulseHandler) team(c fiber.Ctx) error {
	role := middleware.CurrentRole(c)
	if role != domain.RoleManager && role != domain.RoleAdmin && role != domain.RoleHR && role != domain.RolePM {
		return fiber.NewError(fiber.StatusForbidden, "forbidden")
	}
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no employee")
	}
	sum, err := h.svc.Team(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(sum)
}

package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type TimeBreakdownHandler struct {
	svc *service.TimeBreakdownService
}

func NewTimeBreakdownHandler(svc *service.TimeBreakdownService) *TimeBreakdownHandler {
	return &TimeBreakdownHandler{svc: svc}
}

func (h *TimeBreakdownHandler) Mount(r fiber.Router) {
	r.Get("/me/time-breakdown", h.me)
}

func (h *TimeBreakdownHandler) me(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no employee")
	}
	days := 30
	if q := c.Query("days"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	res, err := h.svc.Build(c.Context(), empID, days)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

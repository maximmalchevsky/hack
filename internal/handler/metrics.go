package handler

import (
	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/service"
)

type MetricsHandler struct {
	rec *service.RecommendationService
}

func NewMetricsHandler(rec *service.RecommendationService) *MetricsHandler {
	return &MetricsHandler{rec: rec}
}

func (h *MetricsHandler) Mount(r fiber.Router) {
	r.Get("/metrics/employee/:id", h.byEmployee)
}

func (h *MetricsHandler) byEmployee(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	m, err := h.rec.ComputeMetrics(c.Context(), id)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"employee_id": id,
		"A":           m.A,
		"C":           m.C,
		"L":           m.L,
		"Z":           m.Z,
		"H":           m.H,
		"R":           m.R,
	})
}

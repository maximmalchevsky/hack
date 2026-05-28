package handler

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/repository"
	"worktimesync/internal/service"
	"worktimesync/internal/workers"
)

type ExceptionHandler struct {
	svc      *service.ExceptionService
	enqueuer *workers.Enqueuer
}

func NewExceptionHandler(svc *service.ExceptionService, enq *workers.Enqueuer) *ExceptionHandler {
	return &ExceptionHandler{svc: svc, enqueuer: enq}
}

func (h *ExceptionHandler) triggerRecompute(empID uuid.UUID) {
	if h.enqueuer == nil || empID == uuid.Nil {
		return
	}
	_ = h.enqueuer.EnqueueMetricsRecompute(empID)
	_ = h.enqueuer.EnqueueAIRecommend(empID)
}

func (h *ExceptionHandler) Mount(r fiber.Router) {
	r.Get("/exceptions", h.list)
	r.Post("/exceptions", h.create)
	r.Delete("/exceptions/:id", h.delete)
}

func (h *ExceptionHandler) list(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)

	if q := c.Query("employee_id"); q != "" {
		parsed, err := uuid.Parse(q)
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "invalid employee_id")
		}
		empID = parsed
	}
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "employee_id required")
	}

	from := parseTimeQuery(c, "from")
	to := parseTimeQuery(c, "to")

	list, err := h.svc.List(c.Context(), empID, from, to)
	if err != nil {
		return err
	}
	out := make([]TimeExceptionDTO, 0, len(list))
	for _, e := range list {
		out = append(out, ExceptionToDTO(e))
	}
	return c.JSON(fiber.Map{"exceptions": out})
}

func (h *ExceptionHandler) create(c fiber.Ctx) error {
	var req CreateExceptionRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	e, err := h.svc.Create(c.Context(), service.CreateExceptionInput{
		EmployeeID: empID,
		Kind:       domain.ExceptionKind(req.Kind),
		StartAt:    req.StartAt,
		EndAt:      req.EndAt,
		Comment:    req.Comment,
	})
	if err != nil {
		return mapExceptionErr(err)
	}
	h.triggerRecompute(empID)
	return c.Status(fiber.StatusCreated).JSON(ExceptionToDTO(*e))
}

func (h *ExceptionHandler) delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	if err := h.svc.Delete(c.Context(), id, empID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "exception not found")
		}
		return err
	}
	h.triggerRecompute(empID)
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func mapExceptionErr(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidException):
		return fiber.NewError(fiber.StatusBadRequest, "invalid exception kind")
	case errors.Is(err, service.ErrInvalidRange):
		return fiber.NewError(fiber.StatusBadRequest, "end_at must be after start_at")
	default:
		return err
	}
}

func parseTimeQuery(c fiber.Ctx, key string) time.Time {
	v := c.Query(key)
	if v == "" {
		return time.Time{}
	}
	t, err := time.Parse(time.RFC3339, v)
	if err != nil {
		return time.Time{}
	}
	return t
}

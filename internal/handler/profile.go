package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/repository"
	"worktimesync/internal/service"
	"worktimesync/internal/workers"
)

// ProfileHandler — /api/v1/profiles, /api/v1/me/profile.
type ProfileHandler struct {
	svc      *service.ProfileService
	enqueuer *workers.Enqueuer
}

func NewProfileHandler(svc *service.ProfileService, enq *workers.Enqueuer) *ProfileHandler {
	return &ProfileHandler{svc: svc, enqueuer: enq}
}

// triggerRecompute — после изменений профиля заводим перерасчёт метрик +
// перегенерацию рекомендаций + пересчёт плана задач. Все три — best-effort
// async через Asynq.
//
// Без replan'а плана старые task blocks на бывших рабочих днях остаются
// в calendar_events и отображаются как «вне графика» в heatmap.
func (h *ProfileHandler) triggerRecompute(empID uuid.UUID) {
	if h.enqueuer == nil || empID == uuid.Nil {
		return
	}
	_ = h.enqueuer.EnqueueMetricsRecompute(empID)
	_ = h.enqueuer.EnqueueAIRecommend(empID)
	_ = h.enqueuer.EnqueueTasksReplanOne(empID)
}

// Mount: ожидает, что вызывающий код уже навесил AuthRequired.
func (h *ProfileHandler) Mount(r fiber.Router) {
	r.Get("/profiles/:employee_id", h.getActive)
	r.Get("/profiles/:employee_id/history", h.getHistory)
	r.Put("/profiles/:employee_id", h.update)

	r.Put("/me/profile", h.updateMine)
	r.Post("/me/profile/confirm", h.confirmMine)
}

func (h *ProfileHandler) getActive(c fiber.Ctx) error {
	empID, err := uuid.Parse(c.Params("employee_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid employee_id")
	}
	wp, err := h.svc.Active(c.Context(), empID)
	if err != nil {
		return err
	}
	if wp == nil {
		return c.Status(fiber.StatusNotFound).JSON(ErrorResponse{Error: "profile not found"})
	}
	return c.JSON(ProfileToDTO(*wp))
}

func (h *ProfileHandler) getHistory(c fiber.Ctx) error {
	empID, err := uuid.Parse(c.Params("employee_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid employee_id")
	}
	list, err := h.svc.History(c.Context(), empID)
	if err != nil {
		return err
	}
	out := make([]WorkProfileDTO, 0, len(list))
	for _, wp := range list {
		out = append(out, ProfileToDTO(wp))
	}
	return c.JSON(fiber.Map{"versions": out})
}

func (h *ProfileHandler) update(c fiber.Ctx) error {
	empID, err := uuid.Parse(c.Params("employee_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid employee_id")
	}
	return h.updateFor(c, empID)
}

// updateMine — обновить свой собственный профиль (employee_id из JWT).
func (h *ProfileHandler) updateMine(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	return h.updateFor(c, empID)
}

func (h *ProfileHandler) updateFor(c fiber.Ctx, empID uuid.UUID) error {
	var req UpdateProfileRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}

	wf := domain.WorkFormat(req.WorkFormat)
	actorID := middleware.UserID(c)
	wp, err := h.svc.UpdateProfile(c.Context(), service.UpdateProfileInput{
		EmployeeID: empID,
		DaysOfWeek: DaysToDomain(req.DaysOfWeek),
		Timezone:   req.Timezone,
		WorkFormat: wf,
	}, &actorID)
	if err != nil {
		return mapProfileErr(err)
	}
	h.triggerRecompute(empID)
	return c.Status(fiber.StatusCreated).JSON(ProfileToDTO(*wp))
}

func (h *ProfileHandler) confirmMine(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	if err := h.svc.ConfirmActive(c.Context(), empID); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "employee not found")
		}
		return err
	}
	h.triggerRecompute(empID)
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func mapProfileErr(err error) error {
	switch {
	case errors.Is(err, service.ErrInvalidWorkFormat):
		return fiber.NewError(fiber.StatusBadRequest, "invalid work_format")
	case errors.Is(err, service.ErrInvalidTimeRange):
		return fiber.NewError(fiber.StatusBadRequest, "end must be after start")
	case errors.Is(err, service.ErrInvalidHours):
		return fiber.NewError(fiber.StatusBadRequest, "invalid hours format")
	case errors.Is(err, service.ErrInvalidTimezone):
		return fiber.NewError(fiber.StatusBadRequest, "invalid timezone")
	default:
		return err
	}
}

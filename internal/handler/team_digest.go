package handler

import (
	"encoding/json"
	"fmt"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

// TeamDigestHandler — POST /api/v1/admin/digest/trigger.
// Дёргает сборку digest'ов прямо сейчас (для admin/тестов/демо). В проде
// то же делает Asynq scheduler по понедельникам.
type TeamDigestHandler struct {
	svc           *service.TeamWeeklyDigestService
	notifications *service.NotificationService
}

func NewTeamDigestHandler(svc *service.TeamWeeklyDigestService, notifications *service.NotificationService) *TeamDigestHandler {
	return &TeamDigestHandler{svc: svc, notifications: notifications}
}

func (h *TeamDigestHandler) Mount(r fiber.Router) {
	g := r.Group("/admin", middleware.RequireRole(domain.RoleAdmin))
	g.Post("/digest/trigger", h.trigger)
}

func (h *TeamDigestHandler) trigger(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	userID := middleware.UserID(c)
	if empID == uuid.Nil || userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no employee")
	}

	payload, err := h.svc.Build(c.Context(), empID)
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	md := h.svc.GenerateText(c.Context(), payload)
	payload.Md = md

	raw, _ := json.Marshal(payload)
	_, err = h.notifications.Push(c.Context(), service.CreateInput{
		UserID: userID,
		Kind:   "team_digest",
		Title:  fmt.Sprintf("Дайджест за неделю: %d сотрудников, риск %.2f", payload.TotalEmployees, payload.AvgRiskR),
		Body:   md,
		Link:   "/analytics",
		Payload: map[string]any{
			"digest": json.RawMessage(raw),
		},
	})
	if err != nil {
		return fiber.NewError(fiber.StatusInternalServerError, err.Error())
	}
	return c.JSON(fiber.Map{"ok": true, "digest": payload})
}

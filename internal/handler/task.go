package handler

import (
	"encoding/json"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/middleware"
	"worktimesync/internal/repository"
	"worktimesync/internal/service"
)

// TaskHandler — endpoint'ы /api/v1/me/tasks/*.
//
// GET    /me/tasks                — список задач + слоты + AI-оценки
// POST   /me/tasks/replan         — пересчитать план + (опционально) AI-fill estimate
// PATCH  /me/tasks/:id/estimate   — ручная оценка часов (заменяет AI)
type TaskHandler struct {
	pool    *pgxpool.Pool
	planner *service.TaskPlannerService
	tasks   *repository.TrackerTaskRepo
}

func NewTaskHandler(pool *pgxpool.Pool, planner *service.TaskPlannerService) *TaskHandler {
	return &TaskHandler{
		pool:    pool,
		planner: planner,
		tasks:   repository.NewTrackerTaskRepo(pool),
	}
}

func (h *TaskHandler) Mount(r fiber.Router) {
	g := r.Group("/me/tasks")
	g.Get("/", h.list)
	g.Post("/replan", h.replan)
	g.Patch("/:id/estimate", h.setEstimate)
}

// list — отдаём задачи + текущие слоты. Сами слоты НЕ пересчитываем — UI просит
// replan отдельной кнопкой, чтобы не запускать GigaChat на каждое открытие.
func (h *TaskHandler) list(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	tasks, err := h.tasks.ListByEmployee(c.Context(), repository.ListTasksFilter{
		EmployeeID: empID,
	})
	if err != nil {
		return err
	}

	// Если у нас есть слоты — отдаём вместе с задачами для рендера Gantt'а.
	now := time.Now().UTC()
	from := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	to := from.AddDate(0, 0, service.PlanHorizonDays)
	slots, _ := h.tasks.ListSlots(c.Context(), empID, from, to)
	slotsByTask := map[string][]TaskSlotDTO{}
	for _, s := range slots {
		key := s.TaskID.String()
		slotsByTask[key] = append(slotsByTask[key], TaskSlotDTO{
			Date:  s.Date.Format("2006-01-02"),
			Hours: s.Hours,
		})
	}

	out := make([]TrackerTaskDTO, 0, len(tasks))
	for _, t := range tasks {
		dto := TrackerTaskToDTO(t)
		dto.Slots = slotsByTask[t.ID.String()]
		out = append(out, dto)
	}
	return c.JSON(fiber.Map{
		"tasks":       out,
		"horizon_end": to.Format("2006-01-02"),
	})
}

// replan — POST /me/tasks/replan. Сначала EnsureEstimates для задач без оценки,
// потом полный пересчёт слотов.
func (h *TaskHandler) replan(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	aiCalls, _ := h.planner.EnsureEstimates(c.Context(), empID, 20)
	res, err := h.planner.Plan(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{
		"ai_calls":      aiCalls,
		"total_hours":   res.TotalHours,
		"horizon_end":   res.HorizonEnd.Format("2006-01-02"),
		"planned_tasks": len(res.Tasks),
	})
}

// setEstimate — пользователь принял оценку AI или вписал свою. Пишем в
// estimated_hours (planner потом возьмёт это как manual).
type setEstimateRequest struct {
	Hours float64 `json:"hours"`
}

func (h *TaskHandler) setEstimate(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid task id")
	}
	var req setEstimateRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Hours <= 0 || req.Hours > 500 {
		return fiber.NewError(fiber.StatusBadRequest, "hours must be in (0..500]")
	}
	if err := h.tasks.SetManualEstimate(c.Context(), id, empID, req.Hours); err != nil {
		if err == repository.ErrNotFound {
			return fiber.NewError(fiber.StatusNotFound, "task not found")
		}
		return err
	}
	return c.JSON(fiber.Map{"ok": true, "hours": req.Hours})
}

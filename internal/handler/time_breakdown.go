package handler

import (
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type TimeBreakdownHandler struct {
	svc  *service.TimeBreakdownService
	pool *pgxpool.Pool
}

func NewTimeBreakdownHandler(svc *service.TimeBreakdownService, pool *pgxpool.Pool) *TimeBreakdownHandler {
	return &TimeBreakdownHandler{svc: svc, pool: pool}
}

func (h *TimeBreakdownHandler) Mount(r fiber.Router) {
	r.Get("/me/time-breakdown", h.me)
	r.Get("/teams/:id/time-breakdown", h.team)
}

func (h *TimeBreakdownHandler) team(c fiber.Ctx) error {
	teamID, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "bad team id")
	}
	role := middleware.CurrentRole(c)
	empID := middleware.EmployeeID(c)

	if role != domain.RoleAdmin && role != domain.RoleHR && role != domain.RoleAnalyst {
		var ownerID uuid.UUID
		var isMember bool
		err := h.pool.QueryRow(c.Context(), `
			SELECT t.owner_id,
			       EXISTS(SELECT 1 FROM team_members tm WHERE tm.team_id = t.id AND tm.employee_id = $2) AS is_member
			FROM teams t WHERE t.id = $1
		`, teamID, empID).Scan(&ownerID, &isMember)
		if err != nil {
			if err == pgx.ErrNoRows {
				return fiber.NewError(fiber.StatusNotFound, "team not found")
			}
			return err
		}
		if ownerID != empID && !isMember {
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		}
	}

	days := 30
	if q := c.Query("days"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 365 {
			days = n
		}
	}
	res, err := h.svc.BuildForTeam(c.Context(), teamID, days)
	if err != nil {
		return err
	}
	return c.JSON(res)
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

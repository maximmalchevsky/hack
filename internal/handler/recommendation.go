package handler

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/repository"
	"worktimesync/internal/service"
)

type RecommendationHandler struct {
	svc  *service.RecommendationService
	pool *pgxpool.Pool // для резолва employee → full_name/role/department
}

func NewRecommendationHandler(svc *service.RecommendationService, pool *pgxpool.Pool) *RecommendationHandler {
	return &RecommendationHandler{svc: svc, pool: pool}
}

func (h *RecommendationHandler) Mount(r fiber.Router) {
	g := r.Group("/recommendations")
	g.Get("/", h.list)
	g.Post("/generate", h.generate)
	g.Post("/:id/apply", h.apply)
	g.Post("/:id/dismiss", h.dismiss)
	g.Post("/:id/snooze", h.snooze)
}

func (h *RecommendationHandler) list(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	role := string(middleware.CurrentRole(c))
	scope := service.Scope(c.Query("scope"))
	statuses := []domain.RecommendationStatus{domain.RecStatusNew, domain.RecStatusSeen}

	list, err := h.svc.ListForViewer(c.Context(), empID, role, scope, statuses)
	if err != nil {
		if errors.Is(err, service.ErrScopeForbidden) {
			return fiber.NewError(fiber.StatusForbidden, "scope not allowed for your role")
		}
		return err
	}

	// Для team/all резолвим имена + роль/отдел один раз батчем.
	var empRefs map[uuid.UUID]EmployeeRefDTO
	if scope == service.ScopeTeam || scope == service.ScopeAll {
		empRefs = h.fetchEmployeeRefs(c.Context(), list)
	}

	out := make([]RecommendationDTO, 0, len(list))
	for _, r := range list {
		dto := recToDTO(r)
		if r.EmployeeID != nil {
			dto.EmployeeID = r.EmployeeID
			if ref, ok := empRefs[*r.EmployeeID]; ok {
				refCopy := ref
				dto.Employee = &refCopy
			}
		}
		out = append(out, dto)
	}
	return c.JSON(fiber.Map{"recommendations": out, "scope": string(scope)})
}

// fetchEmployeeRefs — батчем подгружает имена/роли/отделы для всех employee_id,
// встретившихся в рекомендациях. Делает один SQL вместо N+1.
func (h *RecommendationHandler) fetchEmployeeRefs(ctx context.Context, recs []domain.Recommendation) map[uuid.UUID]EmployeeRefDTO {
	ids := make([]uuid.UUID, 0, len(recs))
	seen := map[uuid.UUID]struct{}{}
	for _, r := range recs {
		if r.EmployeeID == nil {
			continue
		}
		if _, ok := seen[*r.EmployeeID]; ok {
			continue
		}
		seen[*r.EmployeeID] = struct{}{}
		ids = append(ids, *r.EmployeeID)
	}
	out := map[uuid.UUID]EmployeeRefDTO{}
	if len(ids) == 0 || h.pool == nil {
		return out
	}
	rows, err := h.pool.Query(ctx, `
		SELECT e.id, u.full_name, u.role, COALESCE(e.department, '')
		FROM employees e
		JOIN users u ON u.id = e.user_id
		WHERE e.id = ANY($1)
	`, ids)
	if err != nil {
		return out
	}
	defer rows.Close()
	for rows.Next() {
		var ref EmployeeRefDTO
		if err := rows.Scan(&ref.ID, &ref.FullName, &ref.Role, &ref.Department); err != nil {
			continue
		}
		out[ref.ID] = ref
	}
	return out
}

func (h *RecommendationHandler) generate(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	list, err := h.svc.Generate(c.Context(), empID)
	if err != nil {
		return err
	}
	out := make([]RecommendationDTO, 0, len(list))
	for _, r := range list {
		out = append(out, recToDTO(r))
	}
	return c.JSON(fiber.Map{"recommendations": out})
}

func (h *RecommendationHandler) apply(c fiber.Ctx) error {
	return h.setStatus(c, true)
}

func (h *RecommendationHandler) dismiss(c fiber.Ctx) error {
	return h.setStatus(c, false)
}

func (h *RecommendationHandler) snooze(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	days := 7
	if q := c.Query("days"); q != "" {
		if n, err := strconv.Atoi(q); err == nil && n > 0 && n <= 90 {
			days = n
		}
	}
	if err := h.svc.Snooze(c.Context(), id, days); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "recommendation not found")
		}
		return err
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *RecommendationHandler) setStatus(c fiber.Ctx, apply bool) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if apply {
		err = h.svc.Apply(c.Context(), id)
	} else {
		err = h.svc.Dismiss(c.Context(), id)
	}
	if err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "recommendation not found")
		}
		return err
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func recToDTO(r domain.Recommendation) RecommendationDTO {
	dto := RecommendationDTO{
		ID:          r.ID,
		Kind:        r.Kind,
		Priority:    string(r.Priority),
		Title:       r.Title,
		Explanation: r.Explanation,
		Status:      string(r.Status),
		GeneratedBy: r.GeneratedBy,
		CreatedAt:   r.CreatedAt,
	}
	if len(r.EvidenceJSON) > 0 {
		var evidence map[string]any
		if err := json.Unmarshal(r.EvidenceJSON, &evidence); err == nil {
			dto.Evidence = evidence
		}
	}
	return dto
}

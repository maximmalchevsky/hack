package handler

import (
	"context"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

// MetricsEnqueuer — минимальный интерфейс для admin-эндпоинта пересчёта.
// Реальная реализация — *workers.Enqueuer. Описан здесь, чтобы handler не
// зависел от пакета workers и его можно было замокать.
type MetricsEnqueuer interface {
	EnqueueMetricsRecompute(employeeID uuid.UUID) error
}

type AdminHandler struct {
	svc      *service.AdminService
	pool     *pgxpool.Pool   // для списка сотрудников при recompute-all
	enqueuer MetricsEnqueuer // может быть nil — тогда endpoint вернёт 503
}

func NewAdminHandler(svc *service.AdminService) *AdminHandler {
	return &AdminHandler{svc: svc}
}

// WithEnqueuer — DI: подключает Asynq enqueuer для admin-операций.
func (h *AdminHandler) WithEnqueuer(pool *pgxpool.Pool, enq MetricsEnqueuer) *AdminHandler {
	h.pool = pool
	h.enqueuer = enq
	return h
}

// Mount монтирует /api/v1/admin/* с middleware RequireRole(admin).
func (h *AdminHandler) Mount(r fiber.Router) {
	g := r.Group("/admin", middleware.RequireRole(domain.RoleAdmin))
	g.Get("/users", h.listUsers)
	g.Patch("/users/:id/role", h.updateRole)
	g.Get("/sources", h.listSources)
	g.Get("/rules", h.getRules)
	g.Put("/rules", h.updateRules)
	g.Get("/system/health", h.health)
	g.Post("/metrics/recompute-all", h.recomputeAllMetrics)
}

// recomputeAllMetrics — ставит каждому сотруднику metrics:recompute в очередь.
// Используется чтобы прогреть metrics_snapshots (после миграции / при пустых
// показателях на /analytics).
func (h *AdminHandler) recomputeAllMetrics(c fiber.Ctx) error {
	if h.enqueuer == nil || h.pool == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "enqueuer not configured")
	}
	ids, err := h.listEmployeeIDs(c.Context())
	if err != nil {
		return err
	}
	queued := 0
	for _, id := range ids {
		if err := h.enqueuer.EnqueueMetricsRecompute(id); err == nil {
			queued++
		}
	}
	return c.JSON(fiber.Map{"queued": queued, "total": len(ids)})
}

func (h *AdminHandler) listEmployeeIDs(ctx context.Context) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(ctx, `SELECT id FROM employees ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []uuid.UUID
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err != nil {
			continue
		}
		out = append(out, id)
	}
	return out, rows.Err()
}

func (h *AdminHandler) listUsers(c fiber.Ctx) error {
	list, err := h.svc.ListUsers(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"users": list})
}

type UpdateRoleRequest struct {
	Role string `json:"role"`
}

func (h *AdminHandler) updateRole(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req UpdateRoleRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.svc.UpdateRole(c.Context(), id, domain.Role(req.Role)); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *AdminHandler) listSources(c fiber.Ctx) error {
	list, err := h.svc.ListIntegrations(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"sources": list})
}

func (h *AdminHandler) getRules(c fiber.Ctx) error {
	w, err := h.svc.GetWeights(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(w)
}

func (h *AdminHandler) updateRules(c fiber.Ctx) error {
	var w service.AnalyticsWeights
	if err := c.Bind().Body(&w); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	if err := h.svc.UpdateWeights(c.Context(), w); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *AdminHandler) health(c fiber.Ctx) error {
	h2, err := h.svc.Health(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(h2)
}

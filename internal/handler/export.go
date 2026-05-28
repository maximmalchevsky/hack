package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type ExportHandler struct {
	svc  *service.ExportService
	pool *pgxpool.Pool
}

func NewExportHandler(svc *service.ExportService, pool *pgxpool.Pool) *ExportHandler {
	return &ExportHandler{svc: svc, pool: pool}
}

func (h *ExportHandler) Mount(r fiber.Router) {
	g := r.Group("/exports")
	g.Get("/:kind", h.download)
}

func (h *ExportHandler) download(c fiber.Ctx) error {
	kind := service.ExportKind(c.Params("kind"))
	opts := parseDatasetOptions(c)

	role := middleware.CurrentRole(c)
	empID := middleware.EmployeeID(c)
	switch role {
	case domain.RoleAdmin, domain.RoleHR:

	case domain.RoleManager, domain.RolePM:

		if empID == uuid.Nil {
			return fiber.NewError(fiber.StatusUnauthorized, "no employee")
		}
		ids, err := h.teamEmpIDs(c, empID)
		if err != nil {
			return fiber.NewError(fiber.StatusInternalServerError, err.Error())
		}
		opts.RestrictEmpIDs = ids
	default:

		return fiber.NewError(fiber.StatusForbidden, "выгрузки доступны только руководителям, HR и админам")
	}

	if c.Query("format") == "json" {
		ds, err := h.svc.BuildDataset(c.Context(), kind, opts)
		if err != nil {
			if errors.Is(err, service.ErrUnknownExportKind) {
				return fiber.NewError(fiber.StatusNotFound, "unknown export kind")
			}
			return err
		}
		return c.JSON(ds)
	}

	if hasFilters(opts) {
		ds, err := h.svc.BuildDataset(c.Context(), kind, opts)
		if err != nil {
			if errors.Is(err, service.ErrUnknownExportKind) {
				return fiber.NewError(fiber.StatusNotFound, "unknown export kind")
			}
			return err
		}
		res, err := h.svc.DatasetToXLSX(ds, "worktime-"+string(kind))
		if err != nil {
			return err
		}
		c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
		c.Set("Content-Disposition", `attachment; filename="`+res.Filename+`"`)
		return c.Send(res.Data)
	}

	res, err := h.svc.Build(c.Context(), kind)
	if err != nil {
		if errors.Is(err, service.ErrUnknownExportKind) {
			return fiber.NewError(fiber.StatusNotFound, "unknown export kind")
		}
		return err
	}
	c.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	c.Set("Content-Disposition", `attachment; filename="`+res.Filename+`"`)
	return c.Send(res.Data)
}

func parseDatasetOptions(c fiber.Ctx) service.DatasetOptions {
	var opts service.DatasetOptions
	if v := c.Query("from"); v != "" {
		if t, err := parseDateQuery(v); err == nil {
			opts.From = &t
		}
	}
	if v := c.Query("to"); v != "" {
		if t, err := parseDateQuery(v); err == nil {

			eod := t.Add(24*time.Hour - time.Nanosecond)
			opts.To = &eod
		}
	}
	if v := c.Query("departments"); v != "" {
		opts.Departments = splitCSV(v)
	}
	if v := c.Query("columns"); v != "" {
		opts.Columns = splitCSV(v)
	}
	if v := c.Query("kinds"); v != "" {
		opts.Kinds = splitCSV(v)
	}
	return opts
}

func parseDateQuery(v string) (time.Time, error) {
	if len(v) == 10 {
		return time.ParseInLocation("2006-01-02", v, time.UTC)
	}
	return time.Parse(time.RFC3339, v)
}

func splitCSV(v string) []string {
	parts := strings.Split(v, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}

func hasFilters(opts service.DatasetOptions) bool {
	return opts.From != nil || opts.To != nil ||
		len(opts.Departments) > 0 || len(opts.Columns) > 0 ||
		len(opts.Kinds) > 0 || len(opts.RestrictEmpIDs) > 0
}

func (h *ExportHandler) teamEmpIDs(c fiber.Ctx, ownerEmpID uuid.UUID) ([]uuid.UUID, error) {
	rows, err := h.pool.Query(c.Context(), `
		SELECT DISTINCT tm.employee_id
		FROM team_members tm
		JOIN teams t ON t.id = tm.team_id
		WHERE t.owner_id = $1
	`, ownerEmpID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []uuid.UUID{}
	for rows.Next() {
		var id uuid.UUID
		if err := rows.Scan(&id); err == nil {
			out = append(out, id)
		}
	}

	out = append(out, ownerEmpID)
	return out, nil
}

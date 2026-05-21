package handler

import (
	"errors"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/service"
)

type ExportHandler struct {
	svc *service.ExportService
}

func NewExportHandler(svc *service.ExportService) *ExportHandler {
	return &ExportHandler{svc: svc}
}

func (h *ExportHandler) Mount(r fiber.Router) {
	g := r.Group("/exports")
	g.Get("/:kind", h.download)
}

// download — выгрузка пресета. Query-параметры:
//
//	?format=json|xlsx (default xlsx)
//	?from=YYYY-MM-DD          — нижняя граница периода
//	?to=YYYY-MM-DD            — верхняя граница (включительно по дню)
//	?departments=Platform,QA  — фильтр по отделам (CSV)
//	?columns=Email,Роль       — оставить только эти колонки (CSV, по именам headers)
//
// Если задан хотя бы один из from/to/departments/columns — XLSX генерится
// через BuildDataset+DatasetToXLSX, чтобы фильтры применились единообразно.
// Иначе — старый быстрый путь Build (без фильтров) для совместимости.
func (h *ExportHandler) download(c fiber.Ctx) error {
	kind := service.ExportKind(c.Params("kind"))
	opts := parseDatasetOptions(c)

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

	// XLSX: если есть фильтры/колонки — через dataset → xlsx, иначе старый путь.
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
			// "to" интерпретируем как конец дня — включительно.
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

// parseDateQuery принимает YYYY-MM-DD или RFC3339.
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
		len(opts.Departments) > 0 || len(opts.Columns) > 0 || len(opts.Kinds) > 0
}

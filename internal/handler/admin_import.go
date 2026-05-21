package handler

import (
	"bytes"

	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type AdminImportHandler struct {
	svc *service.AdminImportService
}

func NewAdminImportHandler(svc *service.AdminImportService) *AdminImportHandler {
	return &AdminImportHandler{svc: svc}
}

func (h *AdminImportHandler) Mount(r fiber.Router) {
	g := r.Group("/admin", middleware.RequireRole(domain.RoleAdmin))
	g.Post("/users/import", h.importUsers)
}

// importUsers — принимает CSV как тело запроса (text/csv) или как файл в form-data поле "file".
func (h *AdminImportHandler) importUsers(c fiber.Ctx) error {
	var data []byte

	// 1) multipart/form-data, поле "file"
	if file, err := c.FormFile("file"); err == nil && file != nil {
		f, err := file.Open()
		if err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "cannot open uploaded file")
		}
		defer f.Close()
		buf := &bytes.Buffer{}
		if _, err := buf.ReadFrom(f); err != nil {
			return fiber.NewError(fiber.StatusBadRequest, "cannot read uploaded file")
		}
		data = buf.Bytes()
	} else {
		// 2) plain body (text/csv)
		data = c.Body()
	}

	if len(data) == 0 {
		return fiber.NewError(fiber.StatusBadRequest, "пустое тело запроса")
	}

	res, err := h.svc.Import(c.Context(), bytes.NewReader(data))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	return c.JSON(res)
}

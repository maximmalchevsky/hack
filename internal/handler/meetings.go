package handler

import (
	"encoding/json"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

// MeetingsHandler — список и отмена созданных встреч.
type MeetingsHandler struct {
	proposal *service.MeetingProposalService
}

func NewMeetingsHandler(proposal *service.MeetingProposalService) *MeetingsHandler {
	return &MeetingsHandler{proposal: proposal}
}

// Mount — /meetings/* под защитой AuthRequired (middleware ставится в server.go).
func (h *MeetingsHandler) Mount(r fiber.Router) {
	g := r.Group("/meetings")
	g.Get("/my", h.list)
	g.Get("/incoming", h.incoming)
	g.Post("/check-conflicts", h.checkConflicts)
	g.Get("/suggest-reschedule", h.suggestReschedule)
	g.Get("/:id/responses", h.responses)
	g.Post("/:id/respond", h.respond)
	g.Put("/:id", h.update)
	g.Delete("/:id", h.cancel)
}

// suggestReschedule — топ-N встреч viewer'а, которые лучше всего перенести,
// с обоснованием. См. service.SuggestReschedule.
//
//	GET /meetings/suggest-reschedule?days=7&top=3
func (h *MeetingsHandler) suggestReschedule(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	role := middleware.CurrentRole(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	days := atoiOr(c.Query("days"), 7)
	topN := atoiOr(c.Query("top"), 3)
	res, err := h.proposal.SuggestReschedule(c.Context(), userID, empID, role, days, topN)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"suggestions": res})
}

func atoiOr(s string, def int) int {
	if s == "" {
		return def
	}
	n := 0
	for _, r := range s {
		if r < '0' || r > '9' {
			return def
		}
		n = n*10 + int(r-'0')
		if n > 1000 {
			return def
		}
	}
	if n == 0 {
		return def
	}
	return n
}

// checkConflictsRequest — body для POST /meetings/check-conflicts.
// Используется UI «Создать вручную» для мягкого предупреждения о занятости.
type checkConflictsRequest struct {
	StartAt     time.Time   `json:"start_at"`
	EndAt       time.Time   `json:"end_at"`
	EmployeeIDs []uuid.UUID `json:"employee_ids"`
}

// checkConflicts — POST /meetings/check-conflicts.
// Возвращает список занятостей по каждому из переданных employee_ids,
// пересекающихся с указанным слотом. UI рендерит как «мягкое» предупреждение,
// не блокирующее создание.
func (h *MeetingsHandler) checkConflicts(c fiber.Ctx) error {
	var req checkConflictsRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.StartAt.IsZero() || req.EndAt.IsZero() {
		return fiber.NewError(fiber.StatusBadRequest, "start_at and end_at required")
	}
	if !req.EndAt.After(req.StartAt) {
		return fiber.NewError(fiber.StatusBadRequest, "end_at must be after start_at")
	}
	if len(req.EmployeeIDs) == 0 {
		return c.JSON(fiber.Map{"conflicts": []any{}})
	}
	conflicts, err := h.proposal.CheckConflicts(c.Context(), req.StartAt, req.EndAt, req.EmployeeIDs)
	if err != nil {
		if errors.Is(err, service.ErrMeetingInvalidRange) {
			return fiber.NewError(fiber.StatusBadRequest, "invalid range")
		}
		return err
	}
	// Заодно проверяем перегруз: сумма часов встреч в неделе + новая.
	// Падать на ошибке не имеем права — UI получит хотя бы конфликты.
	overload, _ := h.proposal.CheckOverload(c.Context(), req.StartAt, req.EndAt, req.EmployeeIDs)
	return c.JSON(fiber.Map{"conflicts": conflicts, "overload": overload})
}

// incoming — приглашения для текущего пользователя.
func (h *MeetingsHandler) incoming(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "no employee")
	}
	res, err := h.proposal.ListIncoming(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"invites": res})
}

// respondRequest — body для POST /meetings/:id/respond.
type respondRequest struct {
	Status     string `json:"status"`      // accepted | declined
	PushYandex bool   `json:"push_yandex"` // только для accept — добавить в свой Яндекс
}

// respond — accept / decline приглашения текущим пользователем.
func (h *MeetingsHandler) respond(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid meeting id")
	}
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "no employee")
	}
	var req respondRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := h.proposal.Respond(c.Context(), id, empID, req.Status, req.PushYandex); err != nil {
		switch {
		case errors.Is(err, service.ErrMeetingResponseNotFound):
			return fiber.NewError(fiber.StatusNotFound, "you are not invited")
		case errors.Is(err, service.ErrMeetingAlreadyCanceled):
			return fiber.NewError(fiber.StatusConflict, "meeting already cancelled")
		case errors.Is(err, service.ErrMeetingResponseInvalid):
			return fiber.NewError(fiber.StatusBadRequest, "invalid status or meeting already passed")
		case errors.Is(err, service.ErrMeetingForbidden):
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		default:
			return err
		}
	}
	return c.JSON(fiber.Map{"ok": true})
}

// responses — список ответов всех участников. Видит инициатор / owner команды / admin / hr.
func (h *MeetingsHandler) responses(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid meeting id")
	}
	userID := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	role := middleware.CurrentRole(c)
	res, err := h.proposal.ResponsesFor(c.Context(), id, userID, empID, role)
	if err != nil {
		switch {
		case errors.Is(err, service.ErrMeetingNotFound):
			return fiber.NewError(fiber.StatusNotFound, "meeting not found")
		case errors.Is(err, service.ErrMeetingForbidden):
			return fiber.NewError(fiber.StatusForbidden, "forbidden")
		default:
			return err
		}
	}
	return c.JSON(fiber.Map{"responses": res})
}

// updateMeetingRequest — все поля optional. Если ничего не задано → 400.
type updateMeetingRequest struct {
	Title   *string    `json:"title,omitempty"`
	StartAt *time.Time `json:"start_at,omitempty"`
	EndAt   *time.Time `json:"end_at,omitempty"`
}

// update — PUT /meetings/:id.
func (h *MeetingsHandler) update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid meeting id")
	}
	userID := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	role := middleware.CurrentRole(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}

	var req updateMeetingRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.Title == nil && req.StartAt == nil && req.EndAt == nil {
		return fiber.NewError(fiber.StatusBadRequest, "nothing to update")
	}

	if err := h.proposal.Update(c.Context(), id, userID, empID, role, service.UpdateMeetingInput{
		Title:   req.Title,
		StartAt: req.StartAt,
		EndAt:   req.EndAt,
	}); err != nil {
		switch {
		case errors.Is(err, service.ErrMeetingNotFound):
			return fiber.NewError(fiber.StatusNotFound, "meeting not found")
		case errors.Is(err, service.ErrMeetingAlreadyCanceled):
			return fiber.NewError(fiber.StatusConflict, "already cancelled")
		case errors.Is(err, service.ErrMeetingForbidden):
			return fiber.NewError(fiber.StatusForbidden, "not allowed")
		case errors.Is(err, service.ErrMeetingInvalidRange),
			errors.Is(err, service.ErrMeetingInvalidUpdate):
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		default:
			return err
		}
	}
	return c.JSON(fiber.Map{"ok": true})
}

// list — список встреч для текущего пользователя (свои + командные для manager).
func (h *MeetingsHandler) list(c fiber.Ctx) error {
	userID := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	role := middleware.CurrentRole(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	res, err := h.proposal.ListMy(c.Context(), userID, empID, role)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"meetings": res})
}

// cancel — DELETE /meetings/:id.
func (h *MeetingsHandler) cancel(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid meeting id")
	}
	userID := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	role := middleware.CurrentRole(c)
	if userID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}

	if err := h.proposal.Cancel(c.Context(), id, userID, empID, role); err != nil {
		switch {
		case errors.Is(err, service.ErrMeetingNotFound):
			return fiber.NewError(fiber.StatusNotFound, "meeting not found")
		case errors.Is(err, service.ErrMeetingAlreadyCanceled):
			return fiber.NewError(fiber.StatusConflict, "already cancelled")
		case errors.Is(err, service.ErrMeetingForbidden):
			return fiber.NewError(fiber.StatusForbidden, "not allowed")
		default:
			return err
		}
	}
	return c.JSON(fiber.Map{"ok": true})
}

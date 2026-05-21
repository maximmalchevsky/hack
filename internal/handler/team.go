package handler

import (
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type TeamHandler struct {
	svc      *service.TeamService
	proposal *service.MeetingProposalService
}

func NewTeamHandler(svc *service.TeamService, proposal *service.MeetingProposalService) *TeamHandler {
	return &TeamHandler{svc: svc, proposal: proposal}
}

type ProposeMeetingRequest struct {
	StartAt time.Time `json:"start_at"`
	EndAt   time.Time `json:"end_at"`
	Title   string    `json:"title,omitempty"`
}

func (h *TeamHandler) proposeMeeting(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req ProposeMeetingRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if h.proposal == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "meeting proposal not configured")
	}
	res, err := h.proposal.Propose(c.Context(), service.ProposeMeetingInput{
		TeamID:        id,
		StartAt:       req.StartAt,
		EndAt:         req.EndAt,
		Title:         req.Title,
		InitiatorUser: middleware.UserID(c),
		InitiatorEmp:  middleware.EmployeeID(c),
	})
	if err != nil {
		if errors.Is(err, service.ErrMeetingInvalidRange) {
			return fiber.NewError(fiber.StatusBadRequest, err.Error())
		}
		return err
	}
	return c.JSON(res)
}

func (h *TeamHandler) Mount(r fiber.Router) {
	g := r.Group("/teams")
	g.Get("/", h.list)
	g.Post("/", h.create)
	g.Get("/:id", h.byID)
	g.Patch("/:id", h.update)
	g.Delete("/:id", h.delete)
	g.Get("/:id/members", h.members)
	g.Post("/:id/members", h.addMember)
	g.Delete("/:id/members/:employee_id", h.removeMember)
	g.Post("/:id/manager", h.setManager)
	g.Get("/:id/availability", h.availability)
	g.Post("/:id/find-window", h.findWindow)
	g.Post("/:id/propose-meeting", h.proposeMeeting)
}

// --- write endpoints ---

type CreateTeamRequest struct {
	Name       string     `json:"name"`
	OwnerEmpID *uuid.UUID `json:"owner_employee_id,omitempty"`
}

type UpdateTeamRequest struct {
	Name       *string    `json:"name,omitempty"`
	OwnerEmpID *uuid.UUID `json:"owner_employee_id,omitempty"`
	// OwnerSet=true означает что поле owner_employee_id присутствовало в запросе
	// (для возможности отвязать владельца через null). Клиенту проще передавать
	// {"owner_employee_id": null} — мы это распарсим как nil + OwnerSet=true.
}

type AddMemberRequest struct {
	EmployeeID uuid.UUID `json:"employee_id"`
}

type SetManagerRequest struct {
	ManagerEmployeeID uuid.UUID `json:"manager_employee_id"`
}

func (h *TeamHandler) create(c fiber.Ctx) error {
	var req CreateTeamRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	t, err := h.svc.Create(c.Context(), service.CreateTeamInput{
		Name:        req.Name,
		OwnerEmpID:  req.OwnerEmpID,
		ViewerRole:  string(middleware.CurrentRole(c)),
		ViewerEmpID: middleware.EmployeeID(c),
	})
	if err != nil {
		return mapTeamErr(err)
	}
	return c.Status(fiber.StatusCreated).JSON(teamToDTO(t))
}

func (h *TeamHandler) update(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	// Парсим вручную, чтобы отличить отсутствующее поле от null.
	raw := map[string]any{}
	if err := c.Bind().Body(&raw); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	in := service.UpdateTeamInput{
		TeamID:      id,
		ViewerRole:  string(middleware.CurrentRole(c)),
		ViewerEmpID: middleware.EmployeeID(c),
	}
	if v, ok := raw["name"]; ok {
		if s, ok := v.(string); ok {
			in.Name = &s
		}
	}
	if v, ok := raw["owner_employee_id"]; ok {
		in.OwnerSet = true
		if v != nil {
			if s, ok := v.(string); ok {
				if uid, perr := uuid.Parse(s); perr == nil {
					in.OwnerEmpID = &uid
				}
			}
		}
	}
	t, err := h.svc.Update(c.Context(), in)
	if err != nil {
		return mapTeamErr(err)
	}
	return c.JSON(teamToDTO(t))
}

func (h *TeamHandler) delete(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.svc.Delete(c.Context(), id,
		string(middleware.CurrentRole(c)), middleware.EmployeeID(c)); err != nil {
		return mapTeamErr(err)
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *TeamHandler) addMember(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req AddMemberRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := h.svc.AddMember(c.Context(), id, req.EmployeeID,
		string(middleware.CurrentRole(c)), middleware.EmployeeID(c)); err != nil {
		return mapTeamErr(err)
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *TeamHandler) removeMember(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	empID, err := uuid.Parse(c.Params("employee_id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid employee_id")
	}
	if err := h.svc.RemoveMember(c.Context(), id, empID,
		string(middleware.CurrentRole(c)), middleware.EmployeeID(c)); err != nil {
		return mapTeamErr(err)
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func (h *TeamHandler) setManager(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req SetManagerRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if err := h.svc.SetManager(c.Context(), id, req.ManagerEmployeeID,
		string(middleware.CurrentRole(c)), middleware.EmployeeID(c)); err != nil {
		return mapTeamErr(err)
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

func mapTeamErr(err error) error {
	switch {
	case errors.Is(err, service.ErrTeamNotFound):
		return fiber.NewError(fiber.StatusNotFound, "team not found")
	case errors.Is(err, service.ErrTeamForbidden):
		return fiber.NewError(fiber.StatusForbidden, "not allowed to manage this team")
	case errors.Is(err, service.ErrTeamNameRequired):
		return fiber.NewError(fiber.StatusBadRequest, "name required")
	default:
		return err
	}
}

func teamToDTO(t *domain.Team) fiber.Map {
	return fiber.Map{
		"id":         t.ID,
		"name":       t.Name,
		"owner_id":   t.OwnerID,
		"created_at": t.CreatedAt,
		"updated_at": t.UpdatedAt,
	}
}

// FindWindowRequest — параметры в теле POST.
type FindWindowRequest struct {
	DurationMin int    `json:"duration_min"`
	Days        int    `json:"days"`
	Timezone    string `json:"tz"`
	TopN        int    `json:"top_n"`
}

func (h *TeamHandler) findWindow(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	var req FindWindowRequest
	_ = c.Bind().Body(&req)

	windows, err := h.svc.FindWindows(c.Context(), service.FindWindowsInput{
		TeamID:      id,
		DurationMin: req.DurationMin,
		Days:        req.Days,
		ViewerTZ:    req.Timezone,
		TopN:        req.TopN,
	})
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"windows": windows})
}

func (h *TeamHandler) list(c fiber.Ctx) error {
	list, err := h.svc.List(c.Context())
	if err != nil {
		return err
	}
	out := make([]fiber.Map, 0, len(list))
	for _, t := range list {
		out = append(out, fiber.Map{
			"id":         t.ID,
			"name":       t.Name,
			"owner_id":   t.OwnerID,
			"created_at": t.CreatedAt,
		})
	}
	return c.JSON(fiber.Map{"teams": out})
}

func (h *TeamHandler) byID(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	t, err := h.svc.ByID(c.Context(), id)
	if err != nil {
		return fiber.NewError(fiber.StatusNotFound, err.Error())
	}
	return c.JSON(fiber.Map{
		"id":         t.ID,
		"name":       t.Name,
		"owner_id":   t.OwnerID,
		"created_at": t.CreatedAt,
	})
}

func (h *TeamHandler) members(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	members, err := h.svc.Members(c.Context(), id)
	if err != nil {
		return err
	}
	out := make([]fiber.Map, 0, len(members))
	for _, m := range members {
		entry := fiber.Map{
			"employee_id": m.EmployeeID,
			"full_name":   m.FullName,
			"role":        m.Role,
			"department":  m.Department,
			"timezone":    m.Timezone,
			"work_format": m.WorkFormat,
		}
		if m.LastProfileUpdateAt != nil {
			entry["last_profile_update_at"] = m.LastProfileUpdateAt
		}
		out = append(out, entry)
	}
	return c.JSON(fiber.Map{"members": out})
}

func (h *TeamHandler) availability(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	tz := c.Query("tz", "Europe/Moscow")
	resp, err := h.svc.Availability(c.Context(), id, tz)
	if err != nil {
		return err
	}
	return c.JSON(resp)
}

// silence unused
var _ = time.Time{}
var _ = middleware.UserID

package handler

import (
	"github.com/gofiber/fiber/v3"

	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type HRRoadmapHandler struct {
	svc      *service.HRRoadmapService
	proposal *service.MeetingProposalService
}

func NewHRRoadmapHandler(svc *service.HRRoadmapService, proposal *service.MeetingProposalService) *HRRoadmapHandler {
	return &HRRoadmapHandler{svc: svc, proposal: proposal}
}

func (h *HRRoadmapHandler) Mount(r fiber.Router) {
	r.Get("/hr/roadmap", h.list)
	r.Post("/hr/notify-stale", h.notifyStale)
}

func (h *HRRoadmapHandler) list(c fiber.Ctx) error {
	items, err := h.svc.Build(c.Context(), 50)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"items": items})
}

// NotifyStaleRequest — параметры массовой рассылки запросов на обновление.
type NotifyStaleRequest struct {
	MinDaysSince int `json:"min_days_since,omitempty"`
}

func (h *HRRoadmapHandler) notifyStale(c fiber.Ctx) error {
	var req NotifyStaleRequest
	_ = c.Bind().Body(&req)
	if req.MinDaysSince == 0 {
		req.MinDaysSince = 60
	}
	if h.proposal == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "notifier not configured")
	}
	res, err := h.proposal.NotifyStaleProfiles(c.Context(), req.MinDaysSince, middleware.UserID(c))
	if err != nil {
		return err
	}
	return c.JSON(res)
}

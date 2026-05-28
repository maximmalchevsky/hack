package handler

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"

	"worktimesync/internal/domain"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
)

type AnalyticsHandler struct {
	dash      *service.AnalyticsDashService
	me        *service.AnalyticsMeService
	team      *service.AnalyticsTeamService
	anomalies *service.AnomaliesService
	forecast  *service.ForecastService
}

func NewAnalyticsHandler(
	dash *service.AnalyticsDashService,
	me *service.AnalyticsMeService,
	team *service.AnalyticsTeamService,
	anomalies *service.AnomaliesService,
	forecast *service.ForecastService,
) *AnalyticsHandler {
	return &AnalyticsHandler{dash: dash, me: me, team: team, anomalies: anomalies, forecast: forecast}
}

func (h *AnalyticsHandler) Mount(r fiber.Router) {
	g := r.Group("/analytics")

	me := g.Group("/me")
	me.Get("/overview", h.meOverview)
	me.Get("/trend", h.meTrend)
	me.Get("/conflicts-by-weekday", h.meConflictsByWeekday)
	me.Get("/hours-by-week", h.meHoursByWeek)

	teamRoles := middleware.RequireRole(
		domain.RoleManager, domain.RolePM, domain.RoleHR, domain.RoleAdmin,
	)
	tg := g.Group("/teams", teamRoles)
	tg.Get("/my", h.teamsMy)
	tg.Get("/overview", h.teamsOverview)
	tg.Get("/risk-by-team", h.teamsRiskByTeam)
	tg.Get("/conflicts-by-weekday", h.teamsConflictsByWeekday)
	tg.Get("/freshness-trend", h.teamsFreshnessTrend)
	tg.Get("/groups-distribution", h.teamsGroupsDistribution)

	companyRoles := middleware.RequireRole(
		domain.RoleAdmin, domain.RoleHR, domain.RolePM, domain.RoleAnalyst,
	)
	g.Get("/overview", companyRoles, h.overview)
	g.Get("/risk-by-team", companyRoles, h.riskByTeam)
	g.Get("/conflicts-by-weekday", companyRoles, h.conflictsByWeekday)
	g.Get("/freshness-trend", companyRoles, h.freshnessTrend)
	g.Get("/groups-distribution", companyRoles, h.groupsDistribution)
	g.Get("/leaderboard", companyRoles, h.leaderboard)
	g.Get("/anomalies", companyRoles, h.anomaliesList)
	g.Get("/forecast", companyRoles, h.forecastList)
}

func (h *AnalyticsHandler) overview(c fiber.Ctx) error {
	res, err := h.dash.Overview(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *AnalyticsHandler) riskByTeam(c fiber.Ctx) error {
	res, err := h.dash.RiskByTeam(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"teams": res})
}

func (h *AnalyticsHandler) conflictsByWeekday(c fiber.Ctx) error {
	res, err := h.dash.ConflictsByWeekday(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"days": res})
}

func (h *AnalyticsHandler) freshnessTrend(c fiber.Ctx) error {
	res, err := h.dash.FreshnessTrend(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"weeks": res})
}

func (h *AnalyticsHandler) groupsDistribution(c fiber.Ctx) error {
	res, err := h.dash.GroupsDistribution(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"groups": res})
}

func (h *AnalyticsHandler) leaderboard(c fiber.Ctx) error {
	res, err := h.dash.Leaderboard(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"teams": res})
}

func (h *AnalyticsHandler) anomaliesList(c fiber.Ctx) error {
	res, err := h.anomalies.Detect(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"anomalies": res})
}

func (h *AnalyticsHandler) forecastList(c fiber.Ctx) error {
	res, err := h.forecast.Build(c.Context())
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"forecast": res})
}

func (h *AnalyticsHandler) meOverview(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.me.Overview(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

func (h *AnalyticsHandler) meTrend(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.me.Trend(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"weeks": res})
}

func (h *AnalyticsHandler) meConflictsByWeekday(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.me.ConflictsByWeekday(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"days": res})
}

func (h *AnalyticsHandler) meHoursByWeek(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.me.HoursByWeek(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"weeks": res})
}

func parseTeamID(c fiber.Ctx) (*uuid.UUID, error) {
	raw := c.Query("team_id")
	if raw == "" {
		return nil, nil
	}
	id, err := uuid.Parse(raw)
	if err != nil {
		return nil, fiber.NewError(fiber.StatusBadRequest, "invalid team_id")
	}
	return &id, nil
}

func teamScopeForbidden(err error) error {
	if errors.Is(err, service.ErrTeamNotOwned) {
		return fiber.NewError(fiber.StatusForbidden, "team is not yours")
	}
	return err
}

func (h *AnalyticsHandler) teamsMy(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.team.TeamsForOwner(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"teams": res})
}

func (h *AnalyticsHandler) teamsOverview(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	teamID, err := parseTeamID(c)
	if err != nil {
		return err
	}
	res, err := h.team.Overview(c.Context(), empID, teamID)
	if err != nil {
		return teamScopeForbidden(err)
	}
	return c.JSON(res)
}

func (h *AnalyticsHandler) teamsRiskByTeam(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	res, err := h.team.RiskByTeam(c.Context(), empID)
	if err != nil {
		return err
	}
	return c.JSON(fiber.Map{"teams": res})
}

func (h *AnalyticsHandler) teamsConflictsByWeekday(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	teamID, err := parseTeamID(c)
	if err != nil {
		return err
	}
	res, err := h.team.ConflictsByWeekday(c.Context(), empID, teamID)
	if err != nil {
		return teamScopeForbidden(err)
	}
	return c.JSON(fiber.Map{"days": res})
}

func (h *AnalyticsHandler) teamsFreshnessTrend(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	teamID, err := parseTeamID(c)
	if err != nil {
		return err
	}
	res, err := h.team.FreshnessTrend(c.Context(), empID, teamID)
	if err != nil {
		return teamScopeForbidden(err)
	}
	return c.JSON(fiber.Map{"weeks": res})
}

func (h *AnalyticsHandler) teamsGroupsDistribution(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusForbidden, "employee id is missing")
	}
	teamID, err := parseTeamID(c)
	if err != nil {
		return err
	}
	res, err := h.team.GroupsDistribution(c.Context(), empID, teamID)
	if err != nil {
		return teamScopeForbidden(err)
	}
	return c.JSON(fiber.Map{"groups": res})
}

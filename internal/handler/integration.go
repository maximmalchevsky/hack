package handler

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"

	"worktimesync/internal/integrations/yandex"
	"worktimesync/internal/middleware"
	"worktimesync/internal/service"
	"worktimesync/internal/workers"
)

type IntegrationHandler struct {
	svc      *service.IntegrationService
	enqueuer *workers.Enqueuer
	yandex   *yandex.Provider
	redis    *redis.Client
	frontURL string
}

func NewIntegrationHandler(
	svc *service.IntegrationService,
	enq *workers.Enqueuer,
	yp *yandex.Provider,
	rdb *redis.Client,
	frontURL string,
) *IntegrationHandler {
	return &IntegrationHandler{
		svc:      svc,
		enqueuer: enq,
		yandex:   yp,
		redis:    rdb,
		frontURL: frontURL,
	}
}

func (h *IntegrationHandler) Mount(r fiber.Router) {
	g := r.Group("/integrations")
	g.Get("/", h.list)
	g.Post("/ical", h.connectICal)
	g.Post("/caldav", h.connectCalDAV)
	g.Post("/jira", h.connectJira)
	g.Get("/oauth/yandex/connect", h.yandexConnect)
	g.Post("/:id/sync", h.sync)
	g.Delete("/:id", h.delete)
}

func (h *IntegrationHandler) MountPublic(r fiber.Router) {
	r.Get("/integrations/oauth/callback/yandex", h.yandexCallback)
}

func (h *IntegrationHandler) yandexConnect(c fiber.Ctx) error {
	if h.yandex == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "yandex oauth not configured")
	}
	uid := middleware.UserID(c)
	empID := middleware.EmployeeID(c)
	if uid == uuid.Nil || empID == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user/employee in context")
	}

	state := randomState()
	if err := h.saveState(c.Context(), state, uid, empID); err != nil {
		return err
	}
	return c.Redirect().To(h.yandex.AuthURL(state))
}

func (h *IntegrationHandler) yandexCallback(c fiber.Ctx) error {
	if h.yandex == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "yandex oauth not configured")
	}
	if errStr := c.Query("error"); errStr != "" {
		return c.Redirect().To(h.frontURL + "/integrations?error=" + errStr)
	}
	code := c.Query("code")
	state := c.Query("state")
	if code == "" || state == "" {
		return fiber.NewError(fiber.StatusBadRequest, "code/state required")
	}

	_, empID, err := h.loadState(c.Context(), state)
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid or expired state")
	}

	tok, err := h.yandex.Authenticate(c.Context(), code)
	if err != nil {
		return c.Redirect().To(h.frontURL + "/integrations?error=auth_failed")
	}

	email, _ := tok.Raw["account_email"].(string)
	integ, err := h.svc.ConnectYandexCalendar(c.Context(), service.ConnectYandexInput{
		EmployeeID:   empID,
		AccessToken:  tok.AccessToken,
		RefreshToken: tok.RefreshToken,
		ExpiresAt:    tok.Expiry,
		AccountEmail: email,
		Label:        "Яндекс Календарь",
	})
	if err != nil {
		return c.Redirect().To(h.frontURL + "/integrations?error=save_failed")
	}
	if h.enqueuer != nil {
		_ = h.enqueuer.EnqueueSyncBackfill(integ.ID)
	}
	return c.Redirect().To(h.frontURL + "/integrations?connected=yandex")
}

func randomState() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

func (h *IntegrationHandler) saveState(ctx context.Context, state string, userID, empID uuid.UUID) error {
	if h.redis == nil {
		return errors.New("oauth state storage not available")
	}
	key := "oauth:state:yandex:" + state
	val := userID.String() + "|" + empID.String()
	return h.redis.Set(ctx, key, val, 5*time.Minute).Err()
}

func (h *IntegrationHandler) loadState(ctx context.Context, state string) (userID, empID uuid.UUID, err error) {
	if h.redis == nil {
		return uuid.Nil, uuid.Nil, errors.New("oauth state storage not available")
	}
	key := "oauth:state:yandex:" + state
	val, rerr := h.redis.GetDel(ctx, key).Result()
	if rerr != nil {
		return uuid.Nil, uuid.Nil, rerr
	}
	parts := splitTwo(val, '|')
	if parts[0] == "" || parts[1] == "" {
		return uuid.Nil, uuid.Nil, errors.New("malformed state value")
	}
	uid, perr := uuid.Parse(parts[0])
	if perr != nil {
		return uuid.Nil, uuid.Nil, perr
	}
	eid, perr := uuid.Parse(parts[1])
	if perr != nil {
		return uuid.Nil, uuid.Nil, perr
	}
	return uid, eid, nil
}

func splitTwo(s string, sep byte) [2]string {
	for i := 0; i < len(s); i++ {
		if s[i] == sep {
			return [2]string{s[:i], s[i+1:]}
		}
	}
	return [2]string{s, ""}
}

func (h *IntegrationHandler) list(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	list, err := h.svc.ListByEmployee(c.Context(), empID)
	if err != nil {
		return err
	}
	out := make([]IntegrationDTO, 0, len(list))
	for _, i := range list {
		out = append(out, IntegrationToDTO(i))
	}
	return c.JSON(fiber.Map{"integrations": out})
}

func (h *IntegrationHandler) connectICal(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	var req ConnectICalRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	integ, err := h.svc.ConnectICal(c.Context(), service.ConnectICalInput{
		EmployeeID: empID,
		FeedURL:    req.FeedURL,
		Label:      req.Label,
	})
	if err != nil {
		return err
	}
	if h.enqueuer != nil {
		_ = h.enqueuer.EnqueueSyncBackfill(integ.ID)
	}
	return c.Status(fiber.StatusCreated).JSON(IntegrationToDTO(*integ))
}

func (h *IntegrationHandler) connectJira(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	var req ConnectJiraRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	integ, err := h.svc.ConnectJira(c.Context(), service.ConnectJiraInput{
		EmployeeID: empID,
		BaseURL:    req.BaseURL,
		Email:      req.Email,
		APIToken:   req.APIToken,
		Label:      req.Label,
	})
	if err != nil {
		if errors.Is(err, service.ErrIntegrationBadInput) {
			return fiber.NewError(fiber.StatusBadRequest, "base_url/email/api_token required")
		}
		return fiber.NewError(fiber.StatusBadRequest, err.Error())
	}
	if h.enqueuer != nil {
		_ = h.enqueuer.EnqueueSyncBackfill(integ.ID)
	}
	return c.Status(fiber.StatusCreated).JSON(IntegrationToDTO(*integ))
}

func (h *IntegrationHandler) connectCalDAV(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	var req ConnectCalDAVRequest
	if err := c.Bind().Body(&req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid request body")
	}
	integ, err := h.svc.ConnectCalDAV(c.Context(), service.ConnectCalDAVInput{
		EmployeeID: empID,
		Endpoint:   req.Endpoint,
		Username:   req.Username,
		Password:   req.Password,
		CalPath:    req.CalPath,
		Label:      req.Label,
	})
	if err != nil {
		if errors.Is(err, service.ErrIntegrationBadInput) {
			return fiber.NewError(fiber.StatusBadRequest, "endpoint/username/password required")
		}
		return err
	}
	if h.enqueuer != nil {
		_ = h.enqueuer.EnqueueSyncBackfill(integ.ID)
	}
	return c.Status(fiber.StatusCreated).JSON(IntegrationToDTO(*integ))
}

func (h *IntegrationHandler) sync(c fiber.Ctx) error {
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if h.enqueuer == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "sync queue not available")
	}
	if err := h.enqueuer.EnqueueSyncIncremental(id); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"queued": true})
}

func (h *IntegrationHandler) delete(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid id")
	}
	if err := h.svc.Delete(c.Context(), id, empID); err != nil {
		if errors.Is(err, service.ErrIntegrationNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "integration not found")
		}
		return err
	}
	return c.Status(fiber.StatusNoContent).Send(nil)
}

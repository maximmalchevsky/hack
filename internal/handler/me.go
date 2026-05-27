package handler

import (
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"

	"worktimesync/internal/middleware"
	"worktimesync/internal/repository"
	"worktimesync/internal/service"
)

// MeHandler — GET /api/v1/me и связанные «свои» эндпоинты.
type MeHandler struct {
	pool      *pgxpool.Pool
	users     *repository.UserRepo
	emps      *repository.EmployeeRepo
	events    *repository.CalendarEventRepo
	profiles  *service.ProfileService
	exception *service.ExceptionService
	summary   *service.WeeklySummaryService // может быть nil
	// Конфиг каналов уведомлений (для отдачи deeplink на фронт).
	tgBotUsername string
}

func NewMeHandler(pool *pgxpool.Pool, ps *service.ProfileService, es *service.ExceptionService, sm *service.WeeklySummaryService) *MeHandler {
	return &MeHandler{
		pool:      pool,
		users:     repository.NewUserRepo(pool),
		emps:      repository.NewEmployeeRepo(pool),
		events:    repository.NewCalendarEventRepo(pool),
		profiles:  ps,
		// заполняется через WithTelegramBotUsername (опционально).
		exception: es,
		summary:   sm,
	}
}

// WeeklySummary — GET /api/v1/me/weekly-summary
func (h *MeHandler) WeeklySummary(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	if h.summary == nil {
		return fiber.NewError(fiber.StatusServiceUnavailable, "summary not configured")
	}
	res, err := h.summary.Build(c.Context(), uid)
	if err != nil {
		return err
	}
	return c.JSON(res)
}

// SetEventCategory — PATCH /api/v1/me/events/:id/category
// Меняет категорию своей встречи. Принимает пустую строку → сбрасывает в NULL,
// тогда при следующем подсчёте «куда уходит время» GigaChat пере-классифицирует.
type setEventCategoryRequest struct {
	Category string `json:"category"`
}

func (h *MeHandler) SetEventCategory(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	id, err := uuid.Parse(c.Params("id"))
	if err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid event id")
	}
	var req setEventCategoryRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	// Валидация: либо пусто (сброс), либо одно из канонических значений.
	if req.Category != "" && !slices.Contains(service.TimeBreakdownCategories, req.Category) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid category")
	}
	if err := h.events.SetCategory(c.Context(), id, empID, req.Category); err != nil {
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "event not found")
		}
		return err
	}
	return c.JSON(fiber.Map{"ok": true, "category": req.Category})
}

// Events — GET /api/v1/me/events?from=...&to=...
// События календаря текущего сотрудника за диапазон. По умолчанию — последние
// 7 дней + следующие 7 дней.
func (h *MeHandler) Events(c fiber.Ctx) error {
	empID := middleware.EmployeeID(c)
	if empID == uuid.Nil {
		return fiber.NewError(fiber.StatusBadRequest, "no employee linked to user")
	}
	from := parseTimeQuery(c, "from")
	to := parseTimeQuery(c, "to")
	if from.IsZero() {
		from = time.Now().AddDate(0, 0, -7)
	}
	if to.IsZero() {
		to = time.Now().AddDate(0, 0, 7)
	}
	events, err := h.events.List(c.Context(), repository.ListEventsFilter{
		EmployeeID: empID,
		From:       from,
		To:         to,
	})
	if err != nil {
		return err
	}
	out := make([]CalendarEventDTO, 0, len(events))
	for _, e := range events {
		out = append(out, EventToDTO(e))
	}
	return c.JSON(fiber.Map{"events": out, "from": from, "to": to})
}

func (h *MeHandler) Get(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user in context")
	}

	user, err := h.users.ByID(c.Context(), uid)
	if err != nil {
		return err
	}

	resp := MeResponse{User: UserToDTO(*user)}

	emp, err := h.emps.ByUserID(c.Context(), uid)
	if err == nil && emp != nil {
		dto := EmployeeToDTO(*emp)
		resp.Employee = &dto

		// активный профиль
		if wp, err := h.profiles.Active(c.Context(), emp.ID); err == nil && wp != nil {
			p := ProfileToDTO(*wp)
			resp.WorkProfile = &p
		}
		// будущие исключения (от сейчас + 30 дней назад)
		from := time.Now().AddDate(0, 0, -30)
		exs, err := h.exception.List(c.Context(), emp.ID, from, time.Time{})
		if err == nil {
			for _, e := range exs {
				resp.Exceptions = append(resp.Exceptions, ExceptionToDTO(e))
			}
		}
	}

	return c.JSON(resp)
}

// WithTelegramBotUsername — DI чтобы /api/v1/me/telegram отдавал deeplink.
func (h *MeHandler) WithTelegramBotUsername(name string) *MeHandler {
	h.tgBotUsername = name
	return h
}

// --- Каналы уведомлений: email / telegram ---

type notificationPrefsResponse struct {
	EmailNotifications    bool     `json:"email_notifications"`
	TelegramNotifications bool     `json:"telegram_notifications"`
	TelegramLinked        bool     `json:"telegram_linked"`
	NotifyKinds           []string `json:"notify_kinds"`
	NotifyMinPriority     string   `json:"notify_min_priority"`
}

// NotificationPrefs — GET текущие настройки каналов.
func (h *MeHandler) NotificationPrefs(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var (
		emailOn  bool
		tgOn     bool
		tgChat   *string
		kinds    []string
		minPrio  string
	)
	if err := h.pool.QueryRow(c.Context(), `
		SELECT email_notifications, telegram_notifications, telegram_chat_id,
		       notify_kinds, notify_min_priority
		FROM users WHERE id = $1
	`, uid).Scan(&emailOn, &tgOn, &tgChat, &kinds, &minPrio); err != nil {
		return err
	}
	if kinds == nil {
		kinds = []string{}
	}
	return c.JSON(notificationPrefsResponse{
		EmailNotifications:    emailOn,
		TelegramNotifications: tgOn,
		TelegramLinked:        tgChat != nil && *tgChat != "",
		NotifyKinds:           kinds,
		NotifyMinPriority:     minPrio,
	})
}

// UpdateNotificationPrefs — PATCH каналы / типы / минимальный приоритет.
type notificationPrefsRequest struct {
	EmailNotifications    *bool     `json:"email_notifications,omitempty"`
	TelegramNotifications *bool     `json:"telegram_notifications,omitempty"`
	NotifyKinds           *[]string `json:"notify_kinds,omitempty"`
	NotifyMinPriority     *string   `json:"notify_min_priority,omitempty"`
}

func (h *MeHandler) UpdateNotificationPrefs(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var req notificationPrefsRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	if req.EmailNotifications == nil && req.TelegramNotifications == nil &&
		req.NotifyKinds == nil && req.NotifyMinPriority == nil {
		return fiber.NewError(fiber.StatusBadRequest, "nothing to update")
	}
	if req.NotifyMinPriority != nil {
		switch *req.NotifyMinPriority {
		case "low", "medium", "high":
		default:
			return fiber.NewError(fiber.StatusBadRequest, "invalid notify_min_priority")
		}
	}
	if _, err := h.pool.Exec(c.Context(), `
		UPDATE users
		SET email_notifications    = COALESCE($1, email_notifications),
		    telegram_notifications = COALESCE($2, telegram_notifications),
		    notify_kinds           = COALESCE($3::text[], notify_kinds),
		    notify_min_priority    = COALESCE($4, notify_min_priority)
		WHERE id = $5
	`, req.EmailNotifications, req.TelegramNotifications,
		req.NotifyKinds, req.NotifyMinPriority, uid); err != nil {
		return err
	}
	return h.NotificationPrefs(c)
}

// TelegramStatus — GET статуса + deeplink.
type telegramStatusResponse struct {
	Linked      bool   `json:"linked"`
	BotUsername string `json:"bot_username,omitempty"`
	DeepLink    string `json:"deep_link,omitempty"`
}

func (h *MeHandler) TelegramStatus(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var tgChat *string
	if err := h.pool.QueryRow(c.Context(), `SELECT telegram_chat_id FROM users WHERE id = $1`, uid).Scan(&tgChat); err != nil {
		return err
	}
	resp := telegramStatusResponse{
		Linked:      tgChat != nil && *tgChat != "",
		BotUsername: h.tgBotUsername,
	}
	if h.tgBotUsername != "" {
		resp.DeepLink = "https://t.me/" + h.tgBotUsername + "?start=" + uid.String()
	}
	return c.JSON(resp)
}

// updateEmailRequest — body для PATCH /me/email.
type updateEmailRequest struct {
	Email string `json:"email"`
}

// UpdateEmail — меняет email текущего пользователя. Email — это же логин;
// после смены логиниться нужно по новому. Подтверждения паролем не требуем
// (по продуктовому решению — для хакатона достаточно).
//
// Валидация:
//   - формат через простую регулярку.
//   - 409 если занят другим пользователем.
func (h *MeHandler) UpdateEmail(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var req updateEmailRequest
	if err := json.Unmarshal(c.Body(), &req); err != nil {
		return fiber.NewError(fiber.StatusBadRequest, "invalid body")
	}
	email := strings.ToLower(strings.TrimSpace(req.Email))
	if !looksLikeEmail(email) {
		return fiber.NewError(fiber.StatusBadRequest, "invalid email")
	}
	if err := h.users.UpdateEmail(c.Context(), uid, email); err != nil {
		if errors.Is(err, repository.ErrEmailTaken) {
			return fiber.NewError(fiber.StatusConflict, "email already taken")
		}
		if errors.Is(err, repository.ErrNotFound) {
			return fiber.NewError(fiber.StatusNotFound, "user not found")
		}
		return err
	}
	return c.JSON(fiber.Map{"ok": true, "email": email})
}

// looksLikeEmail — простейшая проверка формата. Не RFC-точная,
// но отсекает явно битые строки. Для серьёзной валидации — отдельный модуль.
func looksLikeEmail(s string) bool {
	if len(s) < 3 || len(s) > 254 {
		return false
	}
	at := strings.IndexByte(s, '@')
	if at <= 0 || at == len(s)-1 {
		return false
	}
	dot := strings.LastIndexByte(s, '.')
	if dot < at+2 || dot == len(s)-1 {
		return false
	}
	for _, r := range s {
		if r <= ' ' || r == ',' || r == ';' || r == '"' || r == '\'' {
			return false
		}
	}
	return true
}

// TelegramUnlink — DELETE привязки.
func (h *MeHandler) TelegramUnlink(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	if _, err := h.pool.Exec(c.Context(), `
		UPDATE users SET telegram_chat_id = NULL, telegram_notifications = false
		WHERE id = $1
	`, uid); err != nil {
		return err
	}
	return c.JSON(fiber.Map{"ok": true})
}

package handler

import (
	"encoding/json"
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
	EmailNotifications    bool `json:"email_notifications"`
	TelegramNotifications bool `json:"telegram_notifications"`
	TelegramLinked        bool `json:"telegram_linked"`
}

// NotificationPrefs — GET текущие настройки каналов.
func (h *MeHandler) NotificationPrefs(c fiber.Ctx) error {
	uid := middleware.UserID(c)
	if uid == uuid.Nil {
		return fiber.NewError(fiber.StatusUnauthorized, "no user")
	}
	var (
		emailOn bool
		tgOn    bool
		tgChat  *string
	)
	if err := h.pool.QueryRow(c.Context(), `
		SELECT email_notifications, telegram_notifications, telegram_chat_id
		FROM users WHERE id = $1
	`, uid).Scan(&emailOn, &tgOn, &tgChat); err != nil {
		return err
	}
	return c.JSON(notificationPrefsResponse{
		EmailNotifications:    emailOn,
		TelegramNotifications: tgOn,
		TelegramLinked:        tgChat != nil && *tgChat != "",
	})
}

// UpdateNotificationPrefs — PATCH email_notifications/telegram_notifications.
type notificationPrefsRequest struct {
	EmailNotifications    *bool `json:"email_notifications,omitempty"`
	TelegramNotifications *bool `json:"telegram_notifications,omitempty"`
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
	if req.EmailNotifications == nil && req.TelegramNotifications == nil {
		return fiber.NewError(fiber.StatusBadRequest, "nothing to update")
	}
	// Точечный UPDATE — COALESCE на nil-ах.
	if _, err := h.pool.Exec(c.Context(), `
		UPDATE users
		SET email_notifications    = COALESCE($1, email_notifications),
		    telegram_notifications = COALESCE($2, telegram_notifications)
		WHERE id = $3
	`, req.EmailNotifications, req.TelegramNotifications, uid); err != nil {
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

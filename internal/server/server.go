package server

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/gofiber/fiber/v3"
	"github.com/gofiber/fiber/v3/middleware/cors"
	"github.com/gofiber/fiber/v3/middleware/recover"
	"github.com/gofiber/fiber/v3/middleware/requestid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"worktimesync/internal/ai"
	"worktimesync/internal/analytics"
	"worktimesync/internal/config"
	"worktimesync/internal/handler"
	"worktimesync/internal/integrations"
	"worktimesync/internal/integrations/yandex"
	"worktimesync/internal/middleware"
	"worktimesync/internal/notify"
	"worktimesync/internal/service"
	"worktimesync/internal/workers"
	"worktimesync/pkg/auth"
	"worktimesync/pkg/crypto"
)

type Server struct {
	app   *fiber.App
	cfg   *config.Config
	log   zerolog.Logger
	db    *pgxpool.Pool
	redis *redis.Client
	jwt   *auth.Manager

	cipher      *crypto.Cipher
	enqueuer    *workers.Enqueuer
	llm         ai.Client
	registry    *integrations.Registry
}

// Deps — все внешние зависимости сервера.
type Deps struct {
	Config   *config.Config
	Log      zerolog.Logger
	DB       *pgxpool.Pool
	Redis    *redis.Client
	Cipher   *crypto.Cipher
	Enqueuer *workers.Enqueuer
	LLM      ai.Client          // может быть nil — fallback на rules
	Registry *integrations.Registry
}

func New(d Deps) *Server {
	app := fiber.New(fiber.Config{
		AppName:      "worktimesync-api",
		ReadTimeout:  d.Config.HTTP.ReadTimeout,
		WriteTimeout: d.Config.HTTP.WriteTimeout,
		ErrorHandler: errorHandler(d.Log),
	})

	app.Use(recover.New())
	app.Use(requestid.New())
	app.Use(cors.New(cors.Config{
		AllowOrigins:     d.Config.CORS.AllowedOrigins,
		AllowCredentials: true,
		AllowMethods:     []string{"GET", "POST", "PUT", "PATCH", "DELETE", "OPTIONS"},
	}))

	jwtMgr := auth.NewManager(d.Config.JWT.Secret, d.Config.JWT.AccessTTL, d.Config.JWT.RefreshTTL)

	s := &Server{
		app:      app,
		cfg:      d.Config,
		log:      d.Log,
		db:       d.DB,
		redis:    d.Redis,
		jwt:      jwtMgr,
		cipher:   d.Cipher,
		enqueuer: d.Enqueuer,
		llm:      d.LLM,
		registry: d.Registry,
	}
	s.registerRoutes()
	return s
}

func (s *Server) registerRoutes() {
	s.app.Get("/healthz", s.healthz)
	s.app.Get("/readyz", s.readyz)
	s.app.Get("/swagger", s.swaggerUI)
	s.app.Get("/swagger/", s.swaggerUI)
	s.app.Get("/swagger/openapi.yaml", s.openAPISpec)

	api := s.app.Group("/api/v1")
	api.Get("/", func(c fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"name":    "WorkTime Sync API",
			"version": "0.1.0",
		})
	})

	// --- Auth (публичная) ---
	authSvc := service.NewAuthService(s.db, s.jwt)
	authH := handler.NewAuthHandler(authSvc)
	authH.Register(api.Group("/auth"))

	// --- Сервисы домена ---
	auditSvc := service.NewAuditService(s.db, s.log)
	profileSvc := service.NewProfileService(s.db, auditSvc)
	exceptionSvc := service.NewExceptionService(s.db)
	integrationSvc := service.NewIntegrationService(s.db, s.cipher, s.registry)

	rules := ai.NewRuleBased(s.cfg.Risk.FreshnessDDays)
	recommender := ai.NewRecommender(s.llm, rules, s.log)
	weights := analytics.Weights{
		W1: s.cfg.Risk.W1, W2: s.cfg.Risk.W2, W3: s.cfg.Risk.W3,
		W4: s.cfg.Risk.W4, W5: s.cfg.Risk.W5,
		FreshnessDDays: s.cfg.Risk.FreshnessDDays,
	}
	metricsCache := service.NewMetricsCache(s.redis)
	recommendationSvc := service.NewRecommendationService(s.db, recommender, weights, metricsCache)
	diagnosticsSvc := service.NewDiagnosticsService(s.db)
	hrRoadmapSvc := service.NewHRRoadmapService(s.db)
	teamSvc := service.NewTeamService(s.db)
	chatCtxBuilder := service.NewChatContextBuilder(s.db, diagnosticsSvc, hrRoadmapSvc, teamSvc)
	aiChatSvc := service.NewAIChatService(s.db, s.llm, chatCtxBuilder)
	// Транспорты доп. каналов: email + telegram. Если не настроены —
	// .Enabled()==false и они не добавятся.
	emailTransport := notify.NewEmailTransport(
		s.cfg.SMTP.Host, s.cfg.SMTP.Port,
		s.cfg.SMTP.User, s.cfg.SMTP.Pass,
		s.cfg.SMTP.From, s.cfg.App.WebURL,
		s.cfg.SMTP.StartTLS,
	)
	telegramTransport := notify.NewTelegramTransport(s.cfg.Telegram.BotToken, s.cfg.App.WebURL)

	notificationSvc := service.NewNotificationService(s.db, s.redis).
		WithTransports(emailTransport, telegramTransport)
	meetingProposalSvc := service.NewMeetingProposalService(s.db, notificationSvc)
	conflictsSvc := service.NewConflictsService(s.db)
	exportSvc := service.NewExportService(s.db, diagnosticsSvc, conflictsSvc)
	reportPresetSvc := service.NewReportPresetService(s.db)
	analyticsDashSvc := service.NewAnalyticsDashService(s.db, diagnosticsSvc, conflictsSvc, recommendationSvc, weights)
	analyticsMeSvc := service.NewAnalyticsMeService(s.db, weights, conflictsSvc)
	analyticsTeamSvc := service.NewAnalyticsTeamService(s.db, weights, diagnosticsSvc, conflictsSvc)
	anomaliesSvc := service.NewAnomaliesService(s.db)
	forecastSvc := service.NewForecastService(s.db, conflictsSvc)
	pulseSvc := service.NewPulseService(s.db)
	timeBreakdownSvc := service.NewTimeBreakdownService(s.db)
	adminImportSvc := service.NewAdminImportService(s.db)
	viewPresetsSvc := service.NewViewPresetsService(s.db)
	teamDigestSvc := service.NewTeamWeeklyDigestService(s.db, s.llm)
	weeklySummarySvc := service.NewWeeklySummaryService(s.db, s.llm)
	employeeSvc := service.NewEmployeeService(s.db)
	webhookSvc := service.NewWebhookService(s.db, s.registry, s.enqueuer)
	adminSvc := service.NewAdminService(s.db)

	// --- Handlers ---
	meH := handler.NewMeHandler(s.db, profileSvc, exceptionSvc, weeklySummarySvc).
		WithTelegramBotUsername(telegramUsernameIfActive(s.cfg.Telegram.BotToken, s.cfg.Telegram.BotUsername))
	profileH := handler.NewProfileHandler(profileSvc, s.enqueuer)
	exceptionH := handler.NewExceptionHandler(exceptionSvc, s.enqueuer)
	// Yandex Calendar OAuth — опциональный (если в .env не задан client_id, передаём nil).
	var yandexProv *yandex.Provider
	if s.cfg.OAuth.YandexClientID != "" && s.cfg.OAuth.YandexClientSecret != "" {
		yandexProv = yandex.New(yandex.Config{
			ClientID:     s.cfg.OAuth.YandexClientID,
			ClientSecret: s.cfg.OAuth.YandexClientSecret,
			RedirectURL:  s.cfg.OAuth.YandexRedirectURL,
		})
		s.registry.RegisterCalendar(yandexProv)
		// Подключаем запись событий в Yandex Calendar в proposeMeeting-flow.
		meetingProposalSvc.WithYandex(yandexProv, s.cipher)
	}
	integrationH := handler.NewIntegrationHandler(
		integrationSvc, s.enqueuer, yandexProv, s.redis, s.cfg.App.WebURL,
	)
	recommendationH := handler.NewRecommendationHandler(recommendationSvc, s.db)
	diagnosticsH := handler.NewDiagnosticsHandler(diagnosticsSvc, conflictsSvc)
	aiH := handler.NewAIHandler(aiChatSvc)
	notificationH := handler.NewNotificationHandler(notificationSvc, s.redis)
	teamH := handler.NewTeamHandler(teamSvc, meetingProposalSvc)
	meetingsH := handler.NewMeetingsHandler(meetingProposalSvc)
	metricsH := handler.NewMetricsHandler(recommendationSvc)
	conflictsH := handler.NewConflictsHandler(conflictsSvc)
	exportH := handler.NewExportHandler(exportSvc)
	reportPresetsH := handler.NewReportPresetsHandler(reportPresetSvc)
	analyticsDashH := handler.NewAnalyticsHandler(analyticsDashSvc, analyticsMeSvc, analyticsTeamSvc, anomaliesSvc, forecastSvc)
	employeeH := handler.NewEmployeeHandler(employeeSvc)
	hrRoadmapH := handler.NewHRRoadmapHandler(hrRoadmapSvc, meetingProposalSvc)
	webhookH := handler.NewWebhookHandler(webhookSvc)
	adminH := handler.NewAdminHandler(adminSvc)
	auditH := handler.NewAuditHandler(auditSvc)
	pulseH := handler.NewPulseHandler(pulseSvc)
	timeBreakdownH := handler.NewTimeBreakdownHandler(timeBreakdownSvc, s.db)
	adminImportH := handler.NewAdminImportHandler(adminImportSvc)
	viewPresetsH := handler.NewViewPresetsHandler(viewPresetsSvc)
	teamDigestH := handler.NewTeamDigestHandler(teamDigestSvc, notificationSvc)

	// --- Webhooks (без авторизации) ---
	webhookH.Mount(api)

	// --- Public OAuth callback (тоже без авторизации; идентификация через state). ---
	integrationH.MountPublic(api)

	// --- Защищённый префикс ---
	authed := api.Group("/", middleware.AuthRequired(s.jwt))
	authed.Get("/me", meH.Get)
	authed.Get("/me/events", meH.Events)
	authed.Get("/me/weekly-summary", meH.WeeklySummary)
	authed.Get("/me/notification-prefs", meH.NotificationPrefs)
	authed.Patch("/me/notification-prefs", meH.UpdateNotificationPrefs)
	authed.Get("/me/telegram", meH.TelegramStatus)
	authed.Delete("/me/telegram", meH.TelegramUnlink)

	profileH.Mount(authed)
	exceptionH.Mount(authed)
	integrationH.Mount(authed)
	recommendationH.Mount(authed)
	diagnosticsH.Mount(authed)
	aiH.Mount(authed)
	notificationH.Mount(authed)
	teamH.Mount(authed)
	meetingsH.Mount(authed)
	metricsH.Mount(authed)
	conflictsH.Mount(authed)
	employeeH.Mount(authed)
	hrRoadmapH.Mount(authed)
	adminH.Mount(authed)
	auditH.Mount(authed)
	exportH.Mount(authed)
	reportPresetsH.Mount(authed)
	analyticsDashH.Mount(authed)
	pulseH.Mount(authed)
	timeBreakdownH.Mount(authed)
	adminImportH.Mount(authed)
	viewPresetsH.Mount(authed)
	teamDigestH.Mount(authed)
}

func (s *Server) healthz(c fiber.Ctx) error {
	return c.JSON(fiber.Map{"status": "ok"})
}

// swaggerUI отдаёт минимальный HTML с swagger-ui-dist из CDN, читающий /swagger/openapi.yaml.
func (s *Server) swaggerUI(c fiber.Ctx) error {
	c.Set("Content-Type", "text/html; charset=utf-8")
	return c.SendString(`<!DOCTYPE html>
<html><head><title>WorkTime Sync API</title>
<link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui.css">
</head><body>
<div id="swagger-ui"></div>
<script src="https://cdn.jsdelivr.net/npm/swagger-ui-dist@5/swagger-ui-bundle.js"></script>
<script>
window.ui = SwaggerUIBundle({
  url: '/swagger/openapi.yaml',
  dom_id: '#swagger-ui',
  presets: [SwaggerUIBundle.presets.apis],
});
</script>
</body></html>`)
}

// openAPISpec отдаёт сам YAML.
func (s *Server) openAPISpec(c fiber.Ctx) error {
	data, err := openAPIYAML()
	if err != nil {
		return err
	}
	c.Set("Content-Type", "application/yaml")
	return c.Send(data)
}

func (s *Server) readyz(c fiber.Ctx) error {
	ctx, cancel := context.WithTimeout(c.Context(), 2*time.Second)
	defer cancel()

	if err := s.db.Ping(ctx); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "degraded",
			"error":  fmt.Sprintf("postgres: %v", err),
		})
	}
	if err := s.redis.Ping(ctx).Err(); err != nil {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{
			"status": "degraded",
			"error":  fmt.Sprintf("redis: %v", err),
		})
	}
	return c.JSON(fiber.Map{"status": "ready"})
}

func (s *Server) Run(ctx context.Context) error {
	addr := fmt.Sprintf(":%d", s.cfg.HTTP.Port)
	errCh := make(chan error, 1)

	go func() {
		s.log.Info().Str("addr", addr).Msg("HTTP server starting")
		if err := s.app.Listen(addr); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		s.log.Info().Msg("shutdown signal received")
		shutdownCtx, cancel := context.WithTimeout(context.Background(), s.cfg.HTTP.ShutdownTimeout)
		defer cancel()
		return s.app.ShutdownWithContext(shutdownCtx)
	case err := <-errCh:
		return err
	}
}

func errorHandler(log zerolog.Logger) fiber.ErrorHandler {
	return func(c fiber.Ctx, err error) error {
		code := fiber.StatusInternalServerError
		var fe *fiber.Error
		if errors.As(err, &fe) {
			code = fe.Code
		}
		log.Error().
			Err(err).
			Int("status", code).
			Str("path", c.Path()).
			Str("method", c.Method()).
			Msg("request error")
		return c.Status(code).JSON(handler.ErrorResponse{Error: err.Error()})
	}
}

// telegramUsernameIfActive — отдаём username только если бот реально запущен
// (т.е. есть и токен, и имя). Иначе фронт нарисует «бот не настроен», вместо
// неработающей кнопки «Подключить».
func telegramUsernameIfActive(token, username string) string {
	if token == "" || username == "" {
		return ""
	}
	return username
}

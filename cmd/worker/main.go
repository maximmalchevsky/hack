package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/hibiken/asynq"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"worktimesync/internal/ai"
	"worktimesync/internal/analytics"
	"worktimesync/internal/config"
	"worktimesync/internal/integrations/yandex"
	"worktimesync/internal/notifier"
	"worktimesync/internal/notify"
	"worktimesync/internal/service"
	"worktimesync/internal/workers"
	"worktimesync/pkg/crypto"
	"worktimesync/pkg/locks"
	"worktimesync/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic("config: " + err.Error())
	}

	log := logger.New(cfg.App.LogLevel, !cfg.IsProduction())
	log.Info().Str("env", cfg.App.Env).Msg("starting worktimesync-worker")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	// --- Postgres ---
	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()

	pgCfg, err := pgxpool.ParseConfig(cfg.Postgres.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres: parse")
	}
	pgCfg.MaxConns = cfg.Postgres.MaxConns
	pgCfg.MinConns = cfg.Postgres.MinConns
	db, err := pgxpool.NewWithConfig(dbCtx, pgCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres: connect")
	}
	defer db.Close()
	if err := db.Ping(dbCtx); err != nil {
		log.Fatal().Err(err).Msg("postgres: ping")
	}

	// --- Redis ---
	redisOpts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("redis: parse url")
	}
	if cfg.Redis.Password != "" {
		redisOpts.Password = cfg.Redis.Password
	}
	redisOpts.DB = cfg.Redis.DB
	rdb := redis.NewClient(redisOpts)
	defer rdb.Close()
	if err := rdb.Ping(dbCtx).Err(); err != nil {
		log.Fatal().Err(err).Msg("redis: ping")
	}

	// --- Crypto (нужно для расшифровки токенов интеграций) ---
	cipher, err := crypto.NewFromBase64(cfg.App.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("crypto: init")
	}

	// --- Services ---
	syncSvc := service.NewSyncService(db, cipher)
	if cfg.OAuth.YandexClientID != "" && cfg.OAuth.YandexClientSecret != "" {
		syncSvc.WithYandex(yandex.New(yandex.Config{
			ClientID:     cfg.OAuth.YandexClientID,
			ClientSecret: cfg.OAuth.YandexClientSecret,
			RedirectURL:  cfg.OAuth.YandexRedirectURL,
		}))
	}
	lockMgr := locks.NewManager(rdb)

	hrRoadmapSvc := service.NewHRRoadmapService(db)
	// Транспорты доп. каналов: email + telegram. Если не настроены — отключатся сами.
	emailTransport := notify.NewEmailTransport(
		cfg.SMTP.Host, cfg.SMTP.Port,
		cfg.SMTP.User, cfg.SMTP.Pass,
		cfg.SMTP.From, cfg.App.WebURL,
		cfg.SMTP.StartTLS,
	)
	telegramTransport := notify.NewTelegramTransport(cfg.Telegram.BotToken, cfg.App.WebURL)

	notificationSvc := service.NewNotificationService(db, rdb).
		WithTransports(emailTransport, telegramTransport)

	// --- AI (опционально) ---
	var llm ai.Client
	if cfg.GigaChat.ClientID != "" && cfg.GigaChat.ClientSecret != "" {
		gc, err := ai.NewGigaChat(ai.GigaChatConfig{
			ClientID:     cfg.GigaChat.ClientID,
			ClientSecret: cfg.GigaChat.ClientSecret,
			Scope:        cfg.GigaChat.Scope,
			APIURL:       cfg.GigaChat.APIURL,
			OAuthURL:     cfg.GigaChat.OAuthURL,
			Model:        cfg.GigaChat.Model,
		})
		if err != nil {
			log.Warn().Err(err).Msg("gigachat init failed — notifier uses rule-based text")
		} else {
			llm = ai.NewCachedClient(gc, ai.NewResponseCache(rdb))
			log.Info().Msg("gigachat: ready (worker)")
		}
	}

	smartNotifier := notifier.NewSmartNotifier(db, hrRoadmapSvc, notificationSvc, llm, log)

	// --- Recommendation pipeline (та же конфигурация, что в API) ---
	rules := ai.NewRuleBased(cfg.Risk.FreshnessDDays)
	recommender := ai.NewRecommender(llm, rules, log)
	weights := analytics.Weights{
		W1:             cfg.Risk.W1,
		W2:             cfg.Risk.W2,
		W3:             cfg.Risk.W3,
		W4:             cfg.Risk.W4,
		W5:             cfg.Risk.W5,
		FreshnessDDays: cfg.Risk.FreshnessDDays,
	}
	metricsCache := service.NewMetricsCache(rdb)
	recommendationSvc := service.NewRecommendationService(db, recommender, weights, metricsCache)
	teamDigestSvc := service.NewTeamWeeklyDigestService(db, llm)
	meetingPrepSvc := service.NewMeetingPrepService(db, llm)

	// --- Asynq ---
	asynqRedis := asynq.RedisClientOpt{
		Addr:     redisOpts.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	enq := workers.MustNewEnqueuer(redisOpts.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer enq.Close()

	srv := asynq.NewServer(asynqRedis, asynq.Config{
		Concurrency: cfg.Asynq.Concurrency,
		Queues: map[string]int{
			workers.QueueCritical: cfg.Asynq.QueuesCritical,
			workers.QueueDefault:  cfg.Asynq.QueuesDefault,
			workers.QueueLow:      cfg.Asynq.QueuesLow,
		},
		Logger: &asynqLogger{log: log},
	})

	mux := asynq.NewServeMux()
	h := workers.NewHandlers(workers.Deps{
		Log:             log,
		Pool:            db,
		Locks:           lockMgr,
		Sync:            syncSvc,
		Recommendations: recommendationSvc,
		Enqueuer:        enq,
		Notifier:        smartNotifier,
		Notifications:   notificationSvc,
		TeamDigest:      teamDigestSvc,
		MeetingPrep:     meetingPrepSvc,
	})
	h.Register(mux)

	// Telegram-бот (long-polling). Если токен не задан — bot == nil и горутина не запускается.
	pulseSvc := service.NewPulseService(db)
	if tgBot, err := notify.NewBot(cfg.Telegram.BotToken, db, log); err != nil {
		log.Warn().Err(err).Msg("telegram bot init failed")
	} else if tgBot != nil {
		tgBot.WithPulse(pulseSvc)
		go tgBot.Run(ctx)
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Msg("asynq worker starting")
		if err := srv.Run(mux); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info().Msg("worker shutdown signal received")
		srv.Shutdown()
	case err := <-errCh:
		log.Error().Err(err).Msg("worker stopped with error")
		os.Exit(1)
	}
	log.Info().Msg("worker stopped")
}

type asynqLogger struct {
	log zerolog.Logger
}

func (a *asynqLogger) Debug(args ...any) { a.log.Debug().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Info(args ...any)  { a.log.Info().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Warn(args ...any)  { a.log.Warn().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Error(args ...any) { a.log.Error().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Fatal(args ...any) { a.log.Fatal().Msg(fmt.Sprint(args...)) }

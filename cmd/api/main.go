package main

import (
	"context"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"

	"worktimesync/internal/ai"
	"worktimesync/internal/bootstrap"
	"worktimesync/internal/config"
	"worktimesync/internal/integrations"
	"worktimesync/internal/integrations/caldav"
	"worktimesync/internal/integrations/ical"
	"worktimesync/internal/integrations/jira"
	"worktimesync/internal/integrations/yandextracker"
	"worktimesync/internal/server"
	"worktimesync/internal/workers"
	"worktimesync/pkg/crypto"
	"worktimesync/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic("config: " + err.Error())
	}

	log := logger.New(cfg.App.LogLevel, !cfg.IsProduction())
	log.Info().Str("env", cfg.App.Env).Msg("starting worktimesync-api")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	dbCtx, dbCancel := context.WithTimeout(ctx, 10*time.Second)
	defer dbCancel()

	pgCfg, err := pgxpool.ParseConfig(cfg.Postgres.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres: parse config")
	}
	pgCfg.MaxConns = cfg.Postgres.MaxConns
	pgCfg.MinConns = cfg.Postgres.MinConns
	pgCfg.MaxConnLifetime = cfg.Postgres.MaxConnLifetime

	db, err := pgxpool.NewWithConfig(dbCtx, pgCfg)
	if err != nil {
		log.Fatal().Err(err).Msg("postgres: connect")
	}
	defer db.Close()
	if err := db.Ping(dbCtx); err != nil {
		log.Fatal().Err(err).Msg("postgres: ping")
	}
	log.Info().Msg("postgres: connected")

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
	log.Info().Msg("redis: connected")

	// --- Asynq enqueuer (нужен и для bootstrap, и для handler'ов) ---
	enq := workers.MustNewEnqueuer(redisOpts.Addr, cfg.Redis.Password, cfg.Redis.DB)
	defer enq.Close()

	// --- Bootstrap: создание admin + демо-данных при первом старте ---
	// Передаём enqueuer, чтобы после первого seed'а событий автоматически
	// поставить metrics:recompute + ai:recommend для всех сотрудников.
	if err := bootstrap.Run(dbCtx, db, enq, log); err != nil {
		log.Warn().Err(err).Msg("bootstrap: skipped with errors")
	}

	// --- Crypto ---
	cipher, err := crypto.NewFromBase64(cfg.App.EncryptionKey)
	if err != nil {
		log.Fatal().Err(err).Msg("crypto: init")
	}

	// --- Provider registry ---
	registry := integrations.NewRegistry()
	registry.RegisterCalendar(ical.New())
	registry.RegisterCalendar(caldav.New())
	registry.RegisterTracker(jira.New())
	registry.RegisterTracker(yandextracker.New())

	// --- AI (опционально — если GigaChat не настроен, llm = nil) ---
	var llm ai.Client
	if cfg.GigaChat.ClientID != "" && cfg.GigaChat.ClientSecret != "" {
		gcfg := ai.GigaChatConfig{
			ClientID:     cfg.GigaChat.ClientID,
			ClientSecret: cfg.GigaChat.ClientSecret,
			Scope:        cfg.GigaChat.Scope,
			APIURL:       cfg.GigaChat.APIURL,
			OAuthURL:     cfg.GigaChat.OAuthURL,
			Model:        cfg.GigaChat.Model,
		}
		gc, err := ai.NewGigaChat(gcfg)
		if err != nil {
			log.Warn().Err(err).Msg("gigachat init failed — falling back to rule-based recommender")
		} else {
			cache := ai.NewResponseCache(rdb)
			llm = ai.NewCachedClient(gc, cache)
			log.Info().Msg("gigachat: ready (with redis cache)")
		}
	} else {
		log.Info().Msg("gigachat: not configured, using rule-based recommender")
	}

	// --- HTTP server ---
	srv := server.New(server.Deps{
		Config:   cfg,
		Log:      log,
		DB:       db,
		Redis:    rdb,
		Cipher:   cipher,
		Enqueuer: enq,
		LLM:      llm,
		Registry: registry,
	})
	if err := srv.Run(ctx); err != nil {
		log.Fatal().Err(err).Msg("server: run")
	}
	log.Info().Msg("server: stopped")
}

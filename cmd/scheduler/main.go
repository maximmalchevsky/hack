package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/hibiken/asynq"
	"github.com/redis/go-redis/v9"
	"github.com/rs/zerolog"

	"worktimesync/internal/config"
	"worktimesync/internal/workers"
	"worktimesync/pkg/logger"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		panic("config: " + err.Error())
	}

	log := logger.New(cfg.App.LogLevel, !cfg.IsProduction())
	log.Info().Str("env", cfg.App.Env).Msg("starting worktimesync-scheduler")

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	redisOpts, err := redis.ParseURL(cfg.Redis.URL)
	if err != nil {
		log.Fatal().Err(err).Msg("redis: parse url")
	}

	asynqRedis := asynq.RedisClientOpt{
		Addr:     redisOpts.Addr,
		Password: cfg.Redis.Password,
		DB:       cfg.Redis.DB,
	}

	scheduler := asynq.NewScheduler(asynqRedis, &asynq.SchedulerOpts{
		Logger: &asynqLogger{log: log},
	})

	// Регистрируем периодические задачи (cron-style).
	//
	// Документация по cron-spec: https://pkg.go.dev/github.com/hibiken/asynq#Scheduler.Register
	periodic := []struct {
		spec    string
		taskKey string
		queue   string
		payload []byte
	}{
		// Каждые 5 минут пинаем sync-incremental — реальный fan-out по интеграциям
		// делает worker (или отдельный enqueuer-job в спринте 2).
		{"@every 5m", "scheduler:tick:sync-incremental", workers.QueueDefault, nil},

		// Каждые 25 минут — за 5 минут до истечения GigaChat access_token (30m TTL).
		{"@every 25m", workers.TaskOAuthRefreshGigaChat, workers.QueueCritical, nil},

		// Каждый час — smart-notifier разбирает HR-Roadmap и пушит уведомления.
		{"@every 1h", workers.TaskNotificationSend, workers.QueueDefault, nil},

		// Ежедневно в 06:00 UTC — fan-out пересборки рекомендаций по всем сотрудникам.
		{"0 6 * * *", workers.TaskDigestDaily, workers.QueueLow, nil},

		// Каждую минуту — сканируем calendar_events и шлём reminder за 15 мин до старта.
		{"@every 1m", workers.TaskReminderScan, workers.QueueDefault, nil},
	}

	for _, p := range periodic {
		task := asynq.NewTask(p.taskKey, p.payload)
		entryID, err := scheduler.Register(p.spec, task, asynq.Queue(p.queue))
		if err != nil {
			log.Fatal().Err(err).Str("spec", p.spec).Str("task", p.taskKey).Msg("scheduler: register")
		}
		log.Info().Str("entry_id", entryID).Str("spec", p.spec).Str("task", p.taskKey).Msg("scheduler entry registered")
	}

	errCh := make(chan error, 1)
	go func() {
		log.Info().Msg("scheduler starting")
		if err := scheduler.Run(); err != nil {
			errCh <- err
		}
	}()

	select {
	case <-ctx.Done():
		log.Info().Msg("scheduler shutdown signal received")
		scheduler.Shutdown()
	case err := <-errCh:
		log.Error().Err(err).Msg("scheduler stopped with error")
		os.Exit(1)
	}
	log.Info().Msg("scheduler stopped")
}

type asynqLogger struct {
	log zerolog.Logger
}

func (a *asynqLogger) Debug(args ...any) { a.log.Debug().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Info(args ...any)  { a.log.Info().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Warn(args ...any)  { a.log.Warn().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Error(args ...any) { a.log.Error().Msg(fmt.Sprint(args...)) }
func (a *asynqLogger) Fatal(args ...any) { a.log.Fatal().Msg(fmt.Sprint(args...)) }

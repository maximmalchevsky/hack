package workers

// Типы Asynq-задач — единые константы для enqueuer'а и обработчика.
// Префикс домена : действие : сущность.
const (
	TaskSyncIncremental         = "sync:incremental"
	TaskSyncBackfill            = "sync:backfill"
	TaskOAuthRefresh            = "oauth:refresh"
	TaskOAuthRefreshGigaChat    = "oauth:refresh:gigachat"
	TaskMetricsRecompute        = "metrics:recompute"
	TaskTeamAvailabilityRebuild = "team:availability:rebuild"
	TaskAIRecommend             = "ai:recommend"
	TaskNotificationSend        = "notifications:send"
	TaskDigestDaily             = "digest:daily"
	TaskReminderScan            = "reminders:scan" // каждую минуту — событие через 15 мин
	TaskTeamDigestWeekly        = "digest:team-weekly"

)

// Очереди по приоритетам.
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

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

	// TaskSyncTickAll — каждые 5 минут scheduler шлёт эту задачу, worker
	// внутри её обработчика разворачивает её в N задач sync:incremental
	// для всех активных интеграций. Нужен fan-out на уровне worker'а,
	// потому что scheduler не знает список интеграций — он только пинает.
	TaskSyncTickAll = "scheduler:tick:sync-incremental"

	// TaskTasksReplanAll — раз в час; worker дёргает TaskPlannerService.Plan
	// для всех сотрудников с активными Jira/Tracker-интеграциями.
	TaskTasksReplanAll = "scheduler:tick:tasks-replan"
	// TaskTasksAIEstimate — раз в 30 минут; для задач без estimated_hours
	// и ai_estimated_hours дёргает GigaChat.
	TaskTasksAIEstimate = "scheduler:tick:tasks-ai-estimate"
)

// Очереди по приоритетам.
const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

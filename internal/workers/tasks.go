package workers

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
	TaskReminderScan            = "reminders:scan"
	TaskTeamDigestWeekly        = "digest:team-weekly"

	TaskSyncTickAll = "scheduler:tick:sync-incremental"

	TaskTasksReplanAll  = "scheduler:tick:tasks-replan"
	TaskTasksAIEstimate = "scheduler:tick:tasks-ai-estimate"

	TaskTasksReplanOne = "tasks:replan:one"
)

const (
	QueueCritical = "critical"
	QueueDefault  = "default"
	QueueLow      = "low"
)

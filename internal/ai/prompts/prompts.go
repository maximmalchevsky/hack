// Package prompts — встроенные системные промпты для AI-модуля.
//
// Использование:
//
//	import "worktimesync/internal/ai/prompts"
//	systemMsg := prompts.ChatAssistant
package prompts

import _ "embed"

//go:embed chat_assistant.md
var ChatAssistant string

//go:embed recommender.md
var Recommender string

//go:embed smart_notifier.md
var SmartNotifier string

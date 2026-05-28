package prompts

import _ "embed"

//go:embed chat_assistant.md
var ChatAssistant string

//go:embed recommender.md
var Recommender string

//go:embed smart_notifier.md
var SmartNotifier string

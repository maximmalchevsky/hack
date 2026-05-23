package ai

import (
	"context"
	"encoding/json"
	"strings"
)

// TaskEstimateInput — что отдаём модели для оценки одной задачи.
// Description можно укоротить — для оценки достаточно первых ~500 символов.
type TaskEstimateInput struct {
	Title       string
	Description string
	Type        string // Story/Task/Bug/...
	Priority    string // highest/high/medium/low/lowest
}

// TaskEstimate — что возвращает оценщик. Confidence ∈ [0..1].
// Если LLM вернул мусор — возвращаем zero и оценщик считает что не смог.
type TaskEstimate struct {
	Hours      float64 `json:"hours"`
	MinHours   float64 `json:"min"`
	MaxHours   float64 `json:"max"`
	Confidence float64 `json:"confidence"`
	// Source = "ai" или "fallback". Зависит, отвечала ли модель валидным JSON.
	Source string `json:"-"`
}

// TaskEstimator — оборачивает ai.Client промптом «оцени часы на задачу».
// Используется TaskPlannerService для задач без estimated_hours.
type TaskEstimator struct {
	llm Client
}

func NewTaskEstimator(llm Client) *TaskEstimator {
	return &TaskEstimator{llm: llm}
}

// Estimate — синхронный вызов GigaChat. Возвращает (estimate, ok). Если ok=false
// — модель недоступна или ответ кривой; вызывающий должен использовать default.
func (e *TaskEstimator) Estimate(ctx context.Context, in TaskEstimateInput) (TaskEstimate, bool) {
	if e == nil || e.llm == nil {
		return TaskEstimate{}, false
	}
	desc := in.Description
	if len(desc) > 500 {
		desc = desc[:500] + "…"
	}

	system := strings.Join([]string{
		"Ты — опытный технический лид. Оцениваешь, сколько часов нужно опытному инженеру",
		"на конкретную задачу из таск-трекера.",
		"",
		"Отвечай ТОЛЬКО валидным JSON-объектом следующего формата (без markdown, без комментариев):",
		`{"hours": число, "min": число, "max": число, "confidence": число от 0 до 1}`,
		"",
		"hours — твоя точечная оценка (целое или с долей часа, например 1.5).",
		"min/max — реалистичный диапазон (min < hours < max).",
		"confidence — твоя уверенность: 0.9 если задача чёткая, 0.3 если очень мало контекста.",
		"",
		"Опирайся на тип задачи (Bug обычно короче Story), приоритет (highest = чаще сложные),",
		"и слова в описании. Не выдумывай детали, которых нет.",
	}, "\n")

	user := strings.Join([]string{
		"Тип: " + nonEmpty(in.Type, "Task"),
		"Приоритет: " + nonEmpty(in.Priority, "medium"),
		"Заголовок: " + in.Title,
		"Описание: " + nonEmpty(desc, "(нет описания)"),
	}, "\n")

	resp, err := e.llm.Complete(ctx, CompletionRequest{
		Messages: []Message{
			{Role: RoleSystem, Content: system},
			{Role: RoleUser, Content: user},
		},
		Temperature: 0.2,
		MaxTokens:   200,
		JSONMode:    true,
	})
	if err != nil || resp == nil {
		return TaskEstimate{}, false
	}

	raw := strings.TrimSpace(resp.Content)
	raw = strings.TrimPrefix(raw, "```json")
	raw = strings.TrimPrefix(raw, "```")
	raw = strings.TrimSuffix(raw, "```")
	raw = strings.TrimSpace(raw)

	var out TaskEstimate
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return TaskEstimate{}, false
	}
	if out.Hours <= 0 {
		return TaskEstimate{}, false
	}
	// Защита от шизы: ограничим разумным диапазоном (0.25ч — 200ч).
	if out.Hours < 0.25 {
		out.Hours = 0.25
	}
	if out.Hours > 200 {
		out.Hours = 200
	}
	if out.MinHours <= 0 || out.MinHours > out.Hours {
		out.MinHours = out.Hours * 0.7
	}
	if out.MaxHours <= 0 || out.MaxHours < out.Hours {
		out.MaxHours = out.Hours * 1.5
	}
	if out.Confidence < 0 {
		out.Confidence = 0
	}
	if out.Confidence > 1 {
		out.Confidence = 1
	}
	out.Source = "ai"
	return out, true
}

func nonEmpty(s, def string) string {
	if strings.TrimSpace(s) == "" {
		return def
	}
	return s
}

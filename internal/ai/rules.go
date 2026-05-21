package ai

import (
	"fmt"
)

// RuleBased — детерминированный генератор рекомендаций.
// Используется как fallback, если GigaChat недоступен, и как baseline
// при отсутствии доверия к LLM-выходу.
type RuleBased struct {
	freshnessDDays int
}

func NewRuleBased(freshnessDDays int) *RuleBased {
	if freshnessDDays <= 0 {
		freshnessDDays = 90
	}
	return &RuleBased{freshnessDDays: freshnessDDays}
}

// Generate — возвращает 0..N рекомендаций.
// Порядок: по убыванию приоритета.
func (r *RuleBased) Generate(snap EmployeeSnapshot) []Recommendation {
	var out []Recommendation

	// 1. Устаревший график (A < 0.5 или > половины срока)
	if snap.Metrics.A < 0.5 || snap.LastProfileUpdateDaysAgo > r.freshnessDDays/2 {
		priority := "medium"
		if snap.LastProfileUpdateDaysAgo > r.freshnessDDays {
			priority = "high"
		}
		out = append(out, Recommendation{
			Kind:        "update_profile",
			Priority:    priority,
			Title:       "Обновить рабочий профиль",
			Explanation: fmt.Sprintf("Последнее обновление графика — %d дней назад. Это снижает точность планирования встреч.", snap.LastProfileUpdateDaysAgo),
			AIEvidence: map[string]any{
				"metric":             "A",
				"value":              snap.Metrics.A,
				"days_since_update":  snap.LastProfileUpdateDaysAgo,
				"freshness_d_days":   r.freshnessDDays,
			},
			GeneratedBy: "rule",
		})
	}

	// 2. Высокая доля встреч вне графика (C > 0.2)
	if snap.Metrics.C > 0.2 && len(snap.TopEventsOutOfSchedule) > 0 {
		priority := "medium"
		if snap.Metrics.C > 0.4 {
			priority = "high"
		}
		eventIDs := make([]string, 0, len(snap.TopEventsOutOfSchedule))
		for _, e := range snap.TopEventsOutOfSchedule {
			eventIDs = append(eventIDs, e.ID)
		}
		out = append(out, Recommendation{
			Kind:        "move_meeting",
			Priority:    priority,
			Title:       "Перенести встречи вне рабочего времени",
			Explanation: fmt.Sprintf("%.0f%% событий проходят за пределами заявленного графика. Возможно, график устарел или график встреч стоит пересмотреть.", snap.Metrics.C*100),
			AIEvidence: map[string]any{
				"metric": "C",
				"value":  snap.Metrics.C,
				"events": eventIDs,
			},
			GeneratedBy: "rule",
		})
	}

	// 3. Высокая нагрузка (L > 0.8)
	if snap.Metrics.L > 0.8 {
		priority := "high"
		if snap.Metrics.L > 0.95 {
			priority = "critical"
		}
		out = append(out, Recommendation{
			Kind:        "reduce_load",
			Priority:    priority,
			Title:       "Снизить нагрузку",
			Explanation: fmt.Sprintf("Загрузка %.0f%% выше нормы. Не назначайте дополнительные встречи на эту неделю.", snap.Metrics.L*100),
			AIEvidence: map[string]any{
				"metric": "L",
				"value":  snap.Metrics.L,
			},
			GeneratedBy: "rule",
		})
	}

	// 4. Смещение часового пояса (Z > 0.3)
	if snap.Metrics.Z > 0.3 {
		out = append(out, Recommendation{
			Kind:        "check_tz",
			Priority:    "medium",
			Title:       "Проверить часовой пояс",
			Explanation: fmt.Sprintf("В календаре %.0f%% событий идут со смещением больше 1 часа от заявленного TZ %q.", snap.Metrics.Z*100, snap.WorkProfile.Timezone),
			AIEvidence: map[string]any{
				"metric":   "Z",
				"value":    snap.Metrics.Z,
				"declared": snap.WorkProfile.Timezone,
			},
			GeneratedBy: "rule",
		})
	}

	// 5. Расхождение HR-формата и календаря (H > 0)
	if snap.Metrics.H > 0 {
		out = append(out, Recommendation{
			Kind:        "check_hr_data",
			Priority:    "medium",
			Title:       "Проверить данные в HR-системе",
			Explanation: fmt.Sprintf("HR-формат работы %q не соответствует фактическому паттерну событий.", snap.WorkProfile.WorkFormat),
			AIEvidence: map[string]any{
				"metric":      "H",
				"value":       snap.Metrics.H,
				"hr_format":   snap.WorkProfile.WorkFormat,
			},
			GeneratedBy: "rule",
		})
	}

	// 6. Если давно не подтверждали — мягкий запрос
	if snap.LastProfileUpdateDaysAgo > 60 && snap.Metrics.A > 0.5 {
		out = append(out, Recommendation{
			Kind:        "confirm_schedule",
			Priority:    "low",
			Title:       "Подтвердить актуальность графика",
			Explanation: "Профиль выглядит свежим по метрикам, но прошло более 60 дней без явного подтверждения. Стоит подтвердить, что всё актуально.",
			AIEvidence: map[string]any{
				"days_since_update": snap.LastProfileUpdateDaysAgo,
			},
			GeneratedBy: "rule",
		})
	}

	return out
}

package jira

import (
	"regexp"
	"strconv"
	"strings"
)

// ParseHoursFromText — пытается выдрать оценку времени из произвольного текста
// (описание задачи). Возвращает (часы, true) если нашёл; (0, false) иначе.
//
// Поддерживает форматы:
//   - "10 часов", "10 ч", "10ч", "10 час"
//   - "10 hours", "10 hrs", "10 hr", "10h"
//   - "30 минут", "30 мин", "30м", "30 min", "30m"  → переводит в часы (0.5)
//   - "1 день", "2 дня", "1d", "1 day"               → 8ч за день
//   - "1.5 часа", "1,5 ч"                            → дробные ОК
//   - "Время на задачу: 10 часов" — тоже ловит
//
// Берём ПЕРВОЕ найденное значение (если в тексте несколько «10 часов» и
// «20 часов» — приоритет первому, обычно это и есть оценка).
func ParseHoursFromText(text string) (float64, bool) {
	if text == "" {
		return 0, false
	}
	lower := strings.ToLower(text)
	// Заменяем запятую на точку для дробей.
	lower = strings.ReplaceAll(lower, ",", ".")

	// Дни: "1d", "2 дня", "1 day"
	if h, ok := matchNum(lower, daysRe); ok {
		return h * 8, true
	}
	// Часы: "10h", "10ч", "1.5 часа", "10 hours"
	if h, ok := matchNum(lower, hoursRe); ok {
		return h, true
	}
	// Минуты: "30m", "30 мин", "30 minutes"
	if m, ok := matchNum(lower, minutesRe); ok {
		return m / 60.0, true
	}
	return 0, false
}

// Регексы. Капчурят число (целое или дробное) + единицу.
// Едини́цы записаны так, чтобы случайно не ловить, например, «10mb».
var (
	// 10h, 10 час, 10 часов, 10 hours, 10 hrs.
	hoursRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:hours?|hrs?|h\b|час(?:ов|а|у|е)?|ч\b)`)
	// 30m, 30 мин, 30 minutes, 30 minute.
	minutesRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:minutes?|mins?|m\b|мин(?:ут[аыуе]?)?|м\b)`)
	// 1d, 1 day, 2 дня, 3 дней.
	daysRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:days?|d\b|дн(?:я|ей|ю)?|день|дней)`)
)

func matchNum(s string, re *regexp.Regexp) (float64, bool) {
	m := re.FindStringSubmatch(s)
	if len(m) < 2 {
		return 0, false
	}
	v, err := strconv.ParseFloat(m[1], 64)
	if err != nil || v <= 0 {
		return 0, false
	}
	// Защита от шизы: > 1000ч одна задача — игнорируем.
	if v > 1000 {
		return 0, false
	}
	return v, true
}

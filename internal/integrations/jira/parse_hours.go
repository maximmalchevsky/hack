package jira

import (
	"regexp"
	"strconv"
	"strings"
)

func ParseHoursFromText(text string) (float64, bool) {
	if text == "" {
		return 0, false
	}
	lower := strings.ToLower(text)
	lower = strings.ReplaceAll(lower, ",", ".")

	if h, ok := matchNum(lower, daysRe); ok {
		return h * 8, true
	}
	if h, ok := matchNum(lower, hoursRe); ok {
		return h, true
	}
	if m, ok := matchNum(lower, minutesRe); ok {
		return m / 60.0, true
	}
	return 0, false
}

var (
	hoursRe   = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:hours?|hrs?|h\b|час(?:ов|а|у|е)?|ч(?:[^а-яёa-z]|$))`)
	minutesRe = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:minutes?|mins?|m\b|мин(?:ут[аыуе]?)?|м(?:[^а-яёa-z]|$))`)
	daysRe    = regexp.MustCompile(`(?i)(\d+(?:\.\d+)?)\s*(?:days?|d\b|дн(?:я|ей|ю)?|день|дней)`)
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
	if v > 1000 {
		return 0, false
	}
	return v, true
}

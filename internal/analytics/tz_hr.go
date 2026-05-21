package analytics

import (
	"strings"
	"time"

	"worktimesync/internal/domain"
)

// TZDrift — Z ∈ [0, 1]. Доля событий, где фактическая активность сильно расходится
// с заявленным часовым поясом профиля.
//
// Эвристика: пусть workStart_profile — час начала рабочего дня в TZ профиля.
// Если событие начинается раньше workStart−1ч или позже workEnd+1ч (в TZ профиля),
// и при этом DTSTART события несёт явный timezone, отличающийся от профильного —
// это признак того, что сотрудник реально живёт в другом TZ.
//
// Простая реализация: считаем долю событий, чей часовой пояс (event.Timezone)
// не совпадает с profile.Timezone и при этом локальный час события вне ±1ч
// рабочих часов профиля.
func TZDrift(events []domain.CalendarEvent, profile *domain.WorkProfile) float64 {
	if profile == nil || len(events) == 0 {
		return 0
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}

	dh := profile.DaysOfWeek.Mon
	if dh == nil {
		dh = profile.DaysOfWeek.Tue
	}
	if dh == nil {
		dh = profile.DaysOfWeek.Wed
	}
	if dh == nil {
		return 0
	}
	ws, e1 := time.Parse("15:04", dh.Start)
	we, e2 := time.Parse("15:04", dh.End)
	if e1 != nil || e2 != nil {
		return 0
	}

	drift := 0
	total := 0
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if ev.EndAt.Sub(ev.StartAt) > 8*time.Hour {
			continue
		}
		total++

		// 1. если timezone события не указан или совпадает с профилем — не считаем
		evTZ := strings.TrimSpace(ev.Timezone)
		if evTZ == "" || evTZ == profile.Timezone {
			continue
		}

		// 2. локальный час начала в TZ профиля
		localStart := ev.StartAt.In(loc)
		hour := localStart.Hour()

		// 3. вне ±1ч рабочих часов профиля → drift
		if hour < ws.Hour()-1 || hour > we.Hour()+1 {
			drift++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(drift) / float64(total)
}

// HRMismatch — H ∈ [0, 1]. Расхождение HR-формата с фактической картиной событий.
//
// Эвристика:
//   - HR говорит "office" — но events.IsRecurring && weekday-paced (стендапы) идут
//     из разных TZ или поздно вечером → mismatch.
//   - HR говорит "remote" — но события привязаны к одному офисному TZ.
//   - HR говорит "hybrid" — всегда 0 (любая картина допустима).
//
// На дне 6 — упрощённая версия. Полноценный детектор паттерна — отдельная задача.
func HRMismatch(events []domain.CalendarEvent, profile *domain.WorkProfile, hrFormat *domain.WorkFormat) float64 {
	if hrFormat == nil || profile == nil || len(events) == 0 {
		return 0
	}
	hf := *hrFormat
	wf := profile.WorkFormat

	// 1. Простой случай — HR и profile совпадают.
	if hf == wf {
		return 0
	}

	// 2. Hybrid в HR — толерантен ко всему.
	if hf == domain.WorkFormatHybrid {
		return 0
	}

	// 3. HR: office, profile: remote — сильный mismatch.
	if hf == domain.WorkFormatOffice && wf == domain.WorkFormatRemote {
		return 1.0
	}

	// 4. HR: remote, profile: office — обычно не критично, но всё же расхождение.
	if hf == domain.WorkFormatRemote && wf == domain.WorkFormatOffice {
		return 0.6
	}

	// 5. Остальные комбинации — средний mismatch.
	return 0.4
}

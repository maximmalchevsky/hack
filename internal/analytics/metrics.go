// Package analytics — расчёт показателей рабочего времени.
//
// Формулы из ТЗ:
//   - A (актуальность):  A = max(0, 1 − d/D), где d — дни с last_profile_update_at, D — порог (по умолч. 90)
//   - C (конфликты):     C = M_out / M_all (события вне рабочих часов / все события)
//   - L (загрузка):      L = H_busy / H_work (часы занятости / рабочие часы)
//   - Z (TZ-drift):      0..1 — доля событий с активностью вне заявленного TZ ±1ч
//   - H (HR-mismatch):   0..1 — расхождение HR-формата и фактического паттерна
//   - R (интегральный):  R = w1(1−A) + w2*C + w3*L + w4*Z + w5*H
//
// На дне 5 реализованы A/C/L и R. Z и H — день 6 (нужен полный календарь активности).
package analytics

import (
	"time"

	"worktimesync/internal/domain"
)

// Weights — настройка весов риска.
type Weights struct {
	W1, W2, W3, W4, W5 float64
	FreshnessDDays     int
}

func DefaultWeights() Weights {
	return Weights{
		W1: 0.30, W2: 0.25, W3: 0.20, W4: 0.15, W5: 0.10,
		FreshnessDDays: 90,
	}
}

// Freshness — A. days — сколько дней прошло с последнего обновления.
func Freshness(days, dDays int) float64 {
	if dDays <= 0 {
		dDays = 90
	}
	if days <= 0 {
		return 1
	}
	v := 1.0 - float64(days)/float64(dDays)
	if v < 0 {
		return 0
	}
	return v
}

// ConflictsRatio — C. Доля событий, попадающих ВНЕ окна work_profile.
// Учитывает исключения (отпуск/больничный/командировка) — события в эти окна
// считаются "разрешёнными" (тоже не конфликт).
// ConflictsRatio — C. Доля «конфликтных» событий от всех активных.
// Событие считается конфликтным, если выполняется ХОТЯ БЫ ОДНО:
//   - оно вне заявленных рабочих часов (и не покрыто исключением);
//   - оно пересекается по времени с другим событием (double-booking /
//     наслоение встреч).
// Каждое событие учитывается в числителе максимум один раз.
func ConflictsRatio(events []domain.CalendarEvent, profile *domain.WorkProfile, exceptions []domain.TimeException) float64 {
	if len(events) == 0 || profile == nil {
		return 0
	}
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}

	// Собираем активные события (без отменённых/исключённых).
	active := make([]domain.CalendarEvent, 0, len(events))
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		active = append(active, ev)
	}
	if len(active) == 0 {
		return 0
	}

	// Множество индексов событий, пересекающихся хотя бы с одним другим.
	overlapping := make(map[int]bool, len(active))
	for i := 0; i < len(active); i++ {
		for j := i + 1; j < len(active); j++ {
			if active[i].StartAt.Before(active[j].EndAt) && active[j].StartAt.Before(active[i].EndAt) {
				overlapping[i] = true
				overlapping[j] = true
			}
		}
	}

	out := 0
	for i, ev := range active {
		conflict := false
		// Вне рабочих часов (и не покрыто исключением вроде отпуска).
		if !inException(ev, exceptions) && !insideWorkHours(ev, profile, loc) {
			conflict = true
		}
		// Наслоение на другую встречу.
		if overlapping[i] {
			conflict = true
		}
		if conflict {
			out++
		}
	}
	return float64(out) / float64(len(active))
}

// Load — L. Сумма часов занятости / часы рабочего профиля за окно.
//
// Простая модель: считаем только события <= 8 часов (фильтруем "весь день"-события),
// объединяем overlap'ы. Базу рабочих часов берём из профиля × числа дней.
func Load(events []domain.CalendarEvent, profile *domain.WorkProfile, from, to time.Time) float64 {
	if profile == nil || !to.After(from) {
		return 0
	}

	busyHours := 0.0
	intervals := make([][2]time.Time, 0, len(events))
	for _, ev := range events {
		if ev.IsExcluded || ev.Status == domain.EventCancelled {
			continue
		}
		if ev.EndAt.Sub(ev.StartAt) > 8*time.Hour {
			continue
		}
		s, e := ev.StartAt, ev.EndAt
		if s.Before(from) {
			s = from
		}
		if e.After(to) {
			e = to
		}
		if !e.After(s) {
			continue
		}
		intervals = append(intervals, [2]time.Time{s, e})
	}

	merged := mergeIntervals(intervals)
	for _, iv := range merged {
		busyHours += iv[1].Sub(iv[0]).Hours()
	}

	workHours := workHoursInRange(profile, from, to)
	if workHours <= 0 {
		return 0
	}
	v := busyHours / workHours
	if v > 1.5 {
		return 1.5 // отсечь дикие выбросы
	}
	return v
}

// Risk — интегральный риск R. Все компоненты ∈ [0,1].
// Возвращает clamp к [0, 1].
func Risk(a, c, l, z, h float64, w Weights) float64 {
	r := w.W1*(1-a) + w.W2*c + w.W3*l + w.W4*z + w.W5*h
	if r < 0 {
		return 0
	}
	if r > 1 {
		return 1
	}
	return r
}

// --- helpers ---

// insideWorkHours — лежит ли событие целиком в рабочих часах для каждого затронутого дня.
// Используем "хотя бы частично пересекается" → если часть события до или после
// рабочих часов, всё событие считаем конфликтным (грубо, но прозрачно).
func insideWorkHours(ev domain.CalendarEvent, profile *domain.WorkProfile, loc *time.Location) bool {
	start := ev.StartAt.In(loc)
	end := ev.EndAt.In(loc)

	// Если событие пересекает день — считаем конфликтом.
	if !sameDay(start, end) {
		return false
	}

	dh := dayHoursFor(profile.DaysOfWeek, start.Weekday())
	if dh == nil {
		return false // выходной → конфликт
	}

	workStart, ok1 := parseHHMM(dh.Start, start, loc)
	workEnd, ok2 := parseHHMM(dh.End, start, loc)
	if !ok1 || !ok2 {
		return false
	}
	return !start.Before(workStart) && !end.After(workEnd)
}

func dayHoursFor(d domain.DaysOfWeek, wd time.Weekday) *domain.DayHours {
	switch wd {
	case time.Monday:
		return d.Mon
	case time.Tuesday:
		return d.Tue
	case time.Wednesday:
		return d.Wed
	case time.Thursday:
		return d.Thu
	case time.Friday:
		return d.Fri
	case time.Saturday:
		return d.Sat
	case time.Sunday:
		return d.Sun
	}
	return nil
}

func parseHHMM(s string, day time.Time, loc *time.Location) (time.Time, bool) {
	t, err := time.ParseInLocation("15:04", s, loc)
	if err != nil {
		return time.Time{}, false
	}
	return time.Date(day.Year(), day.Month(), day.Day(),
		t.Hour(), t.Minute(), 0, 0, loc), true
}

func sameDay(a, b time.Time) bool {
	ya, ma, da := a.Date()
	yb, mb, db := b.Date()
	return ya == yb && ma == mb && da == db
}

func inException(ev domain.CalendarEvent, excs []domain.TimeException) bool {
	for _, e := range excs {
		if ev.StartAt.Before(e.EndAt) && e.StartAt.Before(ev.EndAt) {
			return true
		}
	}
	return false
}

// mergeIntervals — объединяет пересекающиеся интервалы.
func mergeIntervals(in [][2]time.Time) [][2]time.Time {
	if len(in) == 0 {
		return nil
	}
	// сортируем по началу
	sorted := make([][2]time.Time, len(in))
	copy(sorted, in)
	for i := 1; i < len(sorted); i++ {
		for j := i; j > 0 && sorted[j][0].Before(sorted[j-1][0]); j-- {
			sorted[j], sorted[j-1] = sorted[j-1], sorted[j]
		}
	}

	out := [][2]time.Time{sorted[0]}
	for _, iv := range sorted[1:] {
		last := &out[len(out)-1]
		if !iv[0].After(last[1]) {
			if iv[1].After(last[1]) {
				last[1] = iv[1]
			}
		} else {
			out = append(out, iv)
		}
	}
	return out
}

// workHoursInRange — сумма рабочих часов профиля внутри диапазона [from, to].
func workHoursInRange(profile *domain.WorkProfile, from, to time.Time) float64 {
	loc, _ := time.LoadLocation(profile.Timezone)
	if loc == nil {
		loc = time.UTC
	}
	total := 0.0
	for d := from.In(loc); d.Before(to); d = d.AddDate(0, 0, 1) {
		dh := dayHoursFor(profile.DaysOfWeek, d.Weekday())
		if dh == nil {
			continue
		}
		ws, ok1 := parseHHMM(dh.Start, d, loc)
		we, ok2 := parseHHMM(dh.End, d, loc)
		if !ok1 || !ok2 {
			continue
		}
		// обрезаем по диапазону
		if ws.Before(from) {
			ws = from
		}
		if we.After(to) {
			we = to
		}
		if we.After(ws) {
			total += we.Sub(ws).Hours()
		}
	}
	return total
}

package analytics

import (
	"strings"
	"time"

	"worktimesync/internal/domain"
)

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

		evTZ := strings.TrimSpace(ev.Timezone)
		if evTZ == "" || evTZ == profile.Timezone {
			continue
		}

		localStart := ev.StartAt.In(loc)
		hour := localStart.Hour()

		if hour < ws.Hour()-1 || hour > we.Hour()+1 {
			drift++
		}
	}
	if total == 0 {
		return 0
	}
	return float64(drift) / float64(total)
}

func HRMismatch(events []domain.CalendarEvent, profile *domain.WorkProfile, hrFormat *domain.WorkFormat) float64 {
	if hrFormat == nil || profile == nil || len(events) == 0 {
		return 0
	}
	hf := *hrFormat
	wf := profile.WorkFormat

	if hf == wf {
		return 0
	}

	if hf == domain.WorkFormatHybrid {
		return 0
	}

	if hf == domain.WorkFormatOffice && wf == domain.WorkFormatRemote {
		return 1.0
	}

	if hf == domain.WorkFormatRemote && wf == domain.WorkFormatOffice {
		return 0.6
	}

	return 0.4
}

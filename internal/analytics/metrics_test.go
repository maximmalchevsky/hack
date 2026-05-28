package analytics

import (
	"testing"
	"time"

	"worktimesync/internal/domain"
)

func TestFreshness(t *testing.T) {
	cases := []struct {
		name  string
		days  int
		dDays int
		want  float64
	}{
		{"свежий (0 дней)", 0, 90, 1},
		{"отрицательные дни → 1", -5, 90, 1},
		{"половина срока", 45, 90, 0.5},
		{"ровно порог → 0", 90, 90, 0},
		{"за порогом → clamp 0", 180, 90, 0},
		{"dDays<=0 берёт 90", 45, 0, 0.5},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Freshness(c.days, c.dDays)
			if !almostEqual(got, c.want) {
				t.Errorf("Freshness(%d,%d) = %v, want %v", c.days, c.dDays, got, c.want)
			}
		})
	}
}

func TestRisk(t *testing.T) {
	w := DefaultWeights()
	cases := []struct {
		name          string
		a, c, l, z, h float64
		wantMin       float64
		wantMax       float64
	}{
		{"всё идеально → низкий риск", 1, 0, 0, 0, 0, 0, 0.01},
		{"всё плохо → clamp 1", 0, 1, 1, 1, 1, 0.99, 1},
		{"в диапазоне", 0.5, 0.2, 0.3, 0.1, 0, 0, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := Risk(c.a, c.c, c.l, c.z, c.h, w)
			if got < c.wantMin || got > c.wantMax {
				t.Errorf("Risk = %v, want в [%v,%v]", got, c.wantMin, c.wantMax)
			}
			if got < 0 || got > 1 {
				t.Errorf("Risk = %v вне [0,1]", got)
			}
		})
	}
}

func mondayProfile() *domain.WorkProfile {
	return &domain.WorkProfile{
		Timezone: "UTC",
		DaysOfWeek: domain.DaysOfWeek{
			Mon: &domain.DayHours{Start: "09:00", End: "18:00"},
		},
	}
}

func ev(startHour, startMin, endHour, endMin int) domain.CalendarEvent {
	day := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	return domain.CalendarEvent{
		StartAt: day.Add(time.Duration(startHour)*time.Hour + time.Duration(startMin)*time.Minute),
		EndAt:   day.Add(time.Duration(endHour)*time.Hour + time.Duration(endMin)*time.Minute),
		Status:  domain.EventConfirmed,
	}
}

func TestConflictsRatio(t *testing.T) {
	p := mondayProfile()

	t.Run("нет событий → 0", func(t *testing.T) {
		if got := ConflictsRatio(nil, p, nil); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("nil профиль → 0", func(t *testing.T) {
		if got := ConflictsRatio([]domain.CalendarEvent{ev(10, 0, 11, 0)}, nil, nil); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("событие в графике → 0 конфликтов", func(t *testing.T) {
		got := ConflictsRatio([]domain.CalendarEvent{ev(10, 0, 11, 0)}, p, nil)
		if got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("событие вне рабочих часов → конфликт", func(t *testing.T) {
		got := ConflictsRatio([]domain.CalendarEvent{ev(20, 0, 21, 0)}, p, nil)
		if got != 1 {
			t.Errorf("got %v, want 1", got)
		}
	})

	t.Run("double-booking → оба конфликт", func(t *testing.T) {
		got := ConflictsRatio([]domain.CalendarEvent{ev(10, 0, 11, 0), ev(10, 30, 11, 30)}, p, nil)
		if got != 1 {
			t.Errorf("got %v, want 1 (оба наслаиваются)", got)
		}
	})

	t.Run("отменённые игнорируются", func(t *testing.T) {
		cancelled := ev(20, 0, 21, 0)
		cancelled.Status = domain.EventCancelled
		got := ConflictsRatio([]domain.CalendarEvent{cancelled}, p, nil)
		if got != 0 {
			t.Errorf("got %v, want 0 (отменённые не считаются)", got)
		}
	})
}

func TestLoad(t *testing.T) {
	p := mondayProfile()
	from := time.Date(2024, 1, 8, 0, 0, 0, 0, time.UTC)
	to := from.Add(24 * time.Hour)

	t.Run("нет событий → 0", func(t *testing.T) {
		if got := Load(nil, p, from, to); got != 0 {
			t.Errorf("got %v, want 0", got)
		}
	})

	t.Run("2ч из 9ч ≈ 0.22", func(t *testing.T) {
		got := Load([]domain.CalendarEvent{ev(10, 0, 12, 0)}, p, from, to)
		want := 2.0 / 9.0
		if !almostEqual(got, want) {
			t.Errorf("Load = %v, want ~%v", got, want)
		}
	})

	t.Run("пересекающиеся события не считаются дважды", func(t *testing.T) {
		got := Load([]domain.CalendarEvent{ev(10, 0, 12, 0), ev(11, 0, 13, 0)}, p, from, to)
		want := 3.0 / 9.0
		if !almostEqual(got, want) {
			t.Errorf("Load = %v, want ~%v (merge overlap)", got, want)
		}
	})
}

func almostEqual(a, b float64) bool {
	const eps = 1e-9
	d := a - b
	if d < 0 {
		d = -d
	}
	return d < eps
}

package domain

import "testing"

func TestTaskPriorityRank(t *testing.T) {
	cases := []struct {
		p    TaskPriority
		want int
	}{
		{TaskPriorityHighest, 5},
		{TaskPriorityHigh, 4},
		{TaskPriorityMedium, 3},
		{TaskPriorityLow, 2},
		{TaskPriorityLowest, 1},
		{TaskPriority("неизвестно"), 3},
	}
	for _, c := range cases {
		if got := c.p.Rank(); got != c.want {
			t.Errorf("%q.Rank() = %d, want %d", c.p, got, c.want)
		}
	}
	if !(TaskPriorityHighest.Rank() > TaskPriorityHigh.Rank() &&
		TaskPriorityHigh.Rank() > TaskPriorityMedium.Rank() &&
		TaskPriorityMedium.Rank() > TaskPriorityLow.Rank() &&
		TaskPriorityLow.Rank() > TaskPriorityLowest.Rank()) {
		t.Error("ранги нарушают порядок приоритетов")
	}
}

func TestTaskPriorityIsHigh(t *testing.T) {
	high := []TaskPriority{TaskPriorityHighest, TaskPriorityHigh}
	notHigh := []TaskPriority{TaskPriorityMedium, TaskPriorityLow, TaskPriorityLowest}
	for _, p := range high {
		if !p.IsHigh() {
			t.Errorf("%q.IsHigh() = false, want true", p)
		}
	}
	for _, p := range notHigh {
		if p.IsHigh() {
			t.Errorf("%q.IsHigh() = true, want false", p)
		}
	}
}

func TestNormalizeTaskPriority(t *testing.T) {
	cases := []struct {
		in   string
		want TaskPriority
	}{
		{"Highest", TaskPriorityHighest},
		{"Blocker", TaskPriorityHighest},
		{"Critical", TaskPriorityHighest},
		{"High", TaskPriorityHigh},
		{"Major", TaskPriorityHigh},
		{"Medium", TaskPriorityMedium},
		{"Low", TaskPriorityLow},
		{"Minor", TaskPriorityLow},
		{"Lowest", TaskPriorityLowest},
		{"Trivial", TaskPriorityLowest},
		{"что-то левое", TaskPriorityMedium},
		{"", TaskPriorityMedium},
	}
	for _, c := range cases {
		if got := NormalizeTaskPriority(c.in); got != c.want {
			t.Errorf("NormalizeTaskPriority(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

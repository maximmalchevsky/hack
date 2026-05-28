package jira

import "testing"

func TestParseHoursFromText(t *testing.T) {
	cases := []struct {
		name   string
		text   string
		want   float64
		wantOK bool
	}{
		{"часы по-русски", "оценка 10 часов", 10, true},
		{"часы сокращённо", "нужно 3 ч на это", 3, true},
		{"дробные через точку", "1.5 часа", 1.5, true},
		{"дробные через запятую", "1,5 ч", 1.5, true},
		{"минуты → доли часа", "30 мин", 0.5, true},
		{"день → 8 часов", "1 день", 8, true},
		{"два дня → 16 часов", "2 дня", 16, true},
		{"английские hours", "10 hours", 10, true},
		{"английские h", "5h", 5, true},
		{"пустая строка", "", 0, false},
		{"нет чисел времени", "просто описание задачи", 0, false},
		{"защита от абсурда (>1000ч)", "2000 часов", 0, false},
		{"приоритет дней над часами", "1 день, потом 2 часа", 8, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, ok := ParseHoursFromText(c.text)
			if ok != c.wantOK {
				t.Fatalf("ParseHoursFromText(%q) ok=%v, want %v", c.text, ok, c.wantOK)
			}
			if ok && got != c.want {
				t.Errorf("ParseHoursFromText(%q) = %v, want %v", c.text, got, c.want)
			}
		})
	}
}

package service

import (
	"strings"
	"testing"
	"time"
)

func TestPluralRu(t *testing.T) {
	cases := []struct {
		n    int
		want string
	}{
		{1, "час"},
		{2, "часа"},
		{3, "часа"},
		{4, "часа"},
		{5, "часов"},
		{11, "часов"},
		{21, "час"},
		{22, "часа"},
		{25, "часов"},
		{0, "часов"},
	}
	for _, c := range cases {
		if got := pluralRu(c.n, "час", "часа", "часов"); got != c.want {
			t.Errorf("pluralRu(%d) = %q, want %q", c.n, got, c.want)
		}
	}
}

func TestExceptionTitle(t *testing.T) {
	cases := []struct {
		kind    string
		comment string
		want    string
	}{
		{"vacation", "", "Отпуск"},
		{"sick", "", "Больничный"},
		{"business_trip", "", "Командировка"},
		{"vacation", "до 10 числа", "Отпуск — до 10 числа"},
		{"неизвестный_тип", "", "Недоступен"},
	}
	for _, c := range cases {
		if got := exceptionTitle(c.kind, c.comment); got != c.want {
			t.Errorf("exceptionTitle(%q,%q) = %q, want %q", c.kind, c.comment, got, c.want)
		}
	}
}

func TestValidateProposalCategory(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"Стендапы", "Стендапы"},
		{"  Ревью  ", "Ревью"},
		{"стендапы", "Стендапы"},
		{"", ""},
		{"Левая категория", ""},
	}
	for _, c := range cases {
		if got := validateProposalCategory(c.in); got != c.want {
			t.Errorf("validateProposalCategory(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestCurrentWeekRange(t *testing.T) {
	wed := time.Date(2024, 1, 10, 15, 30, 0, 0, time.UTC)
	from, to := currentWeekRange(wed)

	if from.Weekday() != time.Monday {
		t.Errorf("from.Weekday() = %v, want Monday", from.Weekday())
	}
	if d := to.Sub(from); d != 7*24*time.Hour {
		t.Errorf("длина окна = %v, want 168h", d)
	}
	if from.Hour() != 0 || from.Minute() != 0 {
		t.Errorf("from не в полночь: %v", from)
	}
	sun := time.Date(2024, 1, 14, 23, 0, 0, 0, time.UTC)
	from2, _ := currentWeekRange(sun)
	if !from2.Equal(from) {
		t.Errorf("воскресенье дало другое начало недели: %v vs %v", from2, from)
	}
}

func TestStripMetricValues(t *testing.T) {
	cases := []struct {
		name     string
		in       string
		mustGone []string
		mustStay []string
	}{
		{
			name:     "вырезает A=0.20",
			in:       "Иван Иванов — A=0.20, давно не обновлял",
			mustGone: []string{"A=0.20", "0.20"},
			mustStay: []string{"Иван Иванов", "давно не обновлял"},
		},
		{
			name:     "вырезает z-score",
			in:       "Пётр — z-score = 2.1 за месяц",
			mustGone: []string{"2.1", "z-score"},
			mustStay: []string{"Пётр", "за месяц"},
		},
		{
			name:     "оставляет бизнес-цифры",
			in:       "142 дня без обновления, 3 встречи вне графика",
			mustStay: []string{"142", "3 встречи"},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := stripMetricValues(c.in)
			for _, g := range c.mustGone {
				if strings.Contains(got, g) {
					t.Errorf("в %q осталось %q (должно вырезаться)", got, g)
				}
			}
			for _, s := range c.mustStay {
				if !strings.Contains(got, s) {
					t.Errorf("в %q пропало %q (должно остаться)", got, s)
				}
			}
		})
	}
}

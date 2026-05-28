package imip

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

func sampleInvitation() Invitation {
	return Invitation{
		MeetingID:      uuid.MustParse("11111111-1111-1111-1111-111111111111"),
		Title:          "Демо для команды",
		Description:    "Команда Platform",
		StartAt:        time.Date(2026, 6, 1, 9, 0, 0, 0, time.UTC),
		EndAt:          time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		OrganizerEmail: "invites@workie.app",
		OrganizerName:  "Игорь Климов",
		Attendees: []Attendee{
			{Email: "a@example.com", Name: "Анна"},
			{Email: "", Name: "Пустой"},
		},
		Method: "REQUEST",
	}
}

func TestBuildInvitationRequest(t *testing.T) {
	ics := BuildInvitation(sampleInvitation())

	mustContain := []string{
		"BEGIN:VCALENDAR",
		"END:VCALENDAR",
		"BEGIN:VEVENT",
		"METHOD:REQUEST",
		"11111111-1111-1111-1111-111111111111",
		"Демо для команды",
		"invites@workie.app",
		"a@example.com",
	}
	for _, s := range mustContain {
		if !strings.Contains(ics, s) {
			t.Errorf(".ics не содержит %q.\nПолный вывод:\n%s", s, ics)
		}
	}
	if strings.Contains(ics, "Пустой") {
		t.Error("attendee с пустым email попал в .ics")
	}
}

func TestBuildInvitationCancel(t *testing.T) {
	in := sampleInvitation()
	in.Method = "CANCEL"
	ics := BuildInvitation(in)
	if !strings.Contains(ics, "METHOD:CANCEL") {
		t.Errorf(".ics для отмены должен содержать METHOD:CANCEL.\n%s", ics)
	}
}

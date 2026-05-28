package imip

import (
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/google/uuid"
)

type Attendee struct {
	Email string
	Name  string
}

type Invitation struct {
	MeetingID      uuid.UUID
	Title          string
	Description    string
	StartAt        time.Time
	EndAt          time.Time
	OrganizerEmail string
	OrganizerName  string
	Attendees      []Attendee
	Sequence       int
	Method         string
}

func BuildInvitation(in Invitation) string {
	cal := ics.NewCalendar()
	cal.SetProductId("-//Workie//ru//RU")
	cal.SetVersion("2.0")
	cal.SetCalscale("GREGORIAN")

	method := ics.MethodRequest
	if in.Method == "CANCEL" {
		method = ics.MethodCancel
	}
	cal.SetMethod(method)

	ev := cal.AddEvent(in.MeetingID.String())
	now := time.Now().UTC()
	ev.SetDtStampTime(now)
	ev.SetCreatedTime(now)
	ev.SetModifiedAt(now)
	ev.SetStartAt(in.StartAt.UTC())
	ev.SetEndAt(in.EndAt.UTC())
	ev.SetSummary(in.Title)
	if in.Description != "" {
		ev.SetDescription(in.Description)
	}
	ev.SetSequence(in.Sequence)
	if in.Method == "CANCEL" {
		ev.SetStatus(ics.ObjectStatusCancelled)
	} else {
		ev.SetStatus(ics.ObjectStatusConfirmed)
	}

	organizerOpts := []ics.PropertyParameter{}
	if in.OrganizerName != "" {
		organizerOpts = append(organizerOpts, ics.WithCN(in.OrganizerName))
	}
	ev.SetOrganizer(in.OrganizerEmail, organizerOpts...)

	for _, a := range in.Attendees {
		if a.Email == "" {
			continue
		}
		opts := []ics.PropertyParameter{ics.WithRSVP(true)}
		if a.Name != "" {
			opts = append(opts, ics.WithCN(a.Name))
		}
		ev.AddAttendee(a.Email,
			append([]ics.PropertyParameter{
				ics.CalendarUserTypeIndividual,
				ics.ParticipationStatusNeedsAction,
				ics.ParticipationRoleReqParticipant,
			}, opts...)...,
		)
	}

	return cal.Serialize()
}

// Package imip — генератор iMIP-сообщений (iCalendar Message-Based
// Interoperability Protocol, RFC 6047). Используется для рассылки .ics
// инвайтов на встречу: Gmail/Apple Mail/Outlook автоматически распознают
// прикреплённый .ics файл и показывают пользователю кнопки Accept/Decline.
//
// Этот файл — только построение .ics. Отправка через SMTP — в notify/email.go.
// Парсинг ответов (METHOD:REPLY) — в imip/poller.go.
package imip

import (
	"time"

	ics "github.com/arran4/golang-ical"
	"github.com/google/uuid"
)

// Attendee — один участник встречи, которому уходит инвайт.
// Required — email (как RSVP=TRUE) и Name (для CN= в .ics, чтобы клиент
// показал «Иван Петров», а не «ivan@example.com»).
type Attendee struct {
	Email string
	Name  string
}

// Invitation — входной параметр BuildInvitation.
type Invitation struct {
	MeetingID    uuid.UUID // используется как UID в VEVENT — по нему привязываются REPLY
	Title        string
	Description  string
	StartAt      time.Time
	EndAt        time.Time
	OrganizerEmail string // адрес тех. ящика (IMIP_REPLY_TO); REPLY придёт сюда
	OrganizerName  string // отображаемое имя инициатора (его реальное ФИО)
	Attendees      []Attendee
	// Sequence — счётчик ревизий встречи. 0 для новой, +1 при каждом UPDATE.
	// Без него Gmail/Outlook будут считать второе письмо «копией» первого.
	Sequence int
	// Method — REQUEST (новая/обновление) или CANCEL (отмена). Пусто = REQUEST.
	Method string
}

// BuildInvitation возвращает текст .ics-файла, готовый к вставке в text/calendar
// часть multipart-письма. Возвращает строку (не []byte), чтобы было удобно
// записывать в SMTP DATA.
//
// VEVENT.UID = MeetingID.String() — это ключ, по которому потом IMAP-poller
// найдёт meeting_proposals.id при разборе REPLY.
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

	// ORGANIZER — наш технический ящик. Сюда Gmail/Apple отправят REPLY.
	// CN= — реальное ФИО инициатора, чтобы клиент показал «Игорь Климов»
	// вместо технического email'а.
	organizerOpts := []ics.PropertyParameter{}
	if in.OrganizerName != "" {
		organizerOpts = append(organizerOpts, ics.WithCN(in.OrganizerName))
	}
	ev.SetOrganizer(in.OrganizerEmail, organizerOpts...)

	// Каждый участник — ATTENDEE с RSVP=TRUE, чтобы клиент знал, что ожидается
	// ответ. PARTSTAT=NEEDS-ACTION — стандартное начальное состояние.
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

package caldav

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/emersion/go-webdav"
	cd "github.com/emersion/go-webdav/caldav"

	"worktimesync/internal/integrations"
)

type Provider struct{}

func New() *Provider { return &Provider{} }

func (p *Provider) Name() integrations.Provider { return integrations.ProviderCalDAV }

type AuthPayload struct {
	Endpoint string `json:"endpoint"`
	Username string `json:"username"`
	Password string `json:"password"`
	CalPath  string `json:"cal_path,omitempty"`
}

func (p *Provider) Authenticate(ctx context.Context, authCode string) (*integrations.Token, error) {
	if authCode == "" {
		return nil, errors.New("caldav: empty auth payload")
	}
	var payload AuthPayload
	if err := json.Unmarshal([]byte(authCode), &payload); err != nil {
		return nil, fmt.Errorf("caldav: parse auth payload: %w", err)
	}
	if payload.Endpoint == "" || payload.Username == "" || payload.Password == "" {
		return nil, errors.New("caldav: endpoint/username/password required")
	}

	client, err := newClient(payload)
	if err != nil {
		return nil, err
	}

	homeSet, err := client.FindCalendarHomeSet(ctx, "")
	if err != nil {
		return nil, fmt.Errorf("caldav: find calendar home: %w", err)
	}
	if homeSet == "" {
		return nil, errors.New("caldav: no calendar home set")
	}

	calendars, err := client.FindCalendars(ctx, homeSet)
	if err != nil {
		return nil, fmt.Errorf("caldav: find calendars: %w", err)
	}
	if len(calendars) == 0 {
		return nil, errors.New("caldav: no calendars found")
	}

	if payload.CalPath == "" {
		payload.CalPath = calendars[0].Path
	}

	rawPayload, _ := json.Marshal(payload)
	return &integrations.Token{
		TokenType: "basic",
		Raw: map[string]any{
			"payload":       string(rawPayload),
			"cal_path":      payload.CalPath,
			"discovered_at": time.Now().UTC().Format(time.RFC3339),
		},
	}, nil
}

func (p *Provider) RefreshToken(ctx context.Context, _ *integrations.Token) (*integrations.Token, error) {
	return nil, nil
}

func (p *Provider) FetchEvents(ctx context.Context, token *integrations.Token, from, to time.Time) ([]integrations.Event, error) {
	if token == nil {
		return nil, errors.New("caldav: nil token")
	}
	payloadRaw, ok := token.Raw["payload"].(string)
	if !ok || payloadRaw == "" {
		return nil, errors.New("caldav: missing payload in token")
	}
	var payload AuthPayload
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return nil, fmt.Errorf("caldav: parse payload: %w", err)
	}

	client, err := newClient(payload)
	if err != nil {
		return nil, err
	}

	calPath := payload.CalPath
	if calPath == "" {
		return nil, errors.New("caldav: empty cal_path in token")
	}

	q := &cd.CalendarQuery{
		CompRequest: cd.CalendarCompRequest{
			Name:  "VCALENDAR",
			Comps: []cd.CalendarCompRequest{{Name: "VEVENT"}},
		},
		CompFilter: cd.CompFilter{
			Name: "VCALENDAR",
			Comps: []cd.CompFilter{{
				Name:  "VEVENT",
				Start: from.UTC(),
				End:   to.UTC(),
			}},
		},
	}

	objs, err := client.QueryCalendar(ctx, calPath, q)
	if err != nil {
		return nil, fmt.Errorf("caldav: query: %w", err)
	}

	out := make([]integrations.Event, 0, len(objs))
	for _, obj := range objs {
		for _, vevent := range obj.Data.Events() {
			uid := ""
			if up, _ := vevent.Props.Text("UID"); up != "" {
				uid = up
			}
			if uid == "" {
				continue
			}
			title := ""
			if s, _ := vevent.Props.Text("SUMMARY"); s != "" {
				title = s
			}
			desc := ""
			if s, _ := vevent.Props.Text("DESCRIPTION"); s != "" {
				desc = s
			}
			organizer := ""
			if s, _ := vevent.Props.Text("ORGANIZER"); s != "" {
				organizer = strings.TrimPrefix(s, "mailto:")
			}

			start, err := vevent.DateTimeStart(time.UTC)
			if err != nil {
				continue
			}
			end, err := vevent.DateTimeEnd(time.UTC)
			if err != nil {
				end = start.Add(time.Hour)
			}

			ev := integrations.Event{
				SourceID:    uid,
				Title:       title,
				Description: desc,
				StartAt:     start.UTC(),
				EndAt:       end.UTC(),
				Timezone:    start.Location().String(),
				Organizer:   organizer,
				Status:      "confirmed",
			}

			if rr, _ := vevent.Props.Text("RRULE"); rr != "" {
				ev.IsRecurring = true
				ev.RRule = rr
				ev.RecurrenceRoot = uid
			}

			out = append(out, ev)
		}
	}
	return out, nil
}

func (p *Provider) RegisterWebhook(ctx context.Context, _ *integrations.Token, _ string) (string, error) {
	return "", integrations.ErrWebhookNotSupported
}

func (p *Provider) UnregisterWebhook(ctx context.Context, _ *integrations.Token, _ string) error {
	return nil
}

func (p *Provider) ParseWebhook(r *http.Request) (*integrations.WebhookEvent, error) {
	return nil, integrations.ErrWebhookNotSupported
}

func newClient(p AuthPayload) (*cd.Client, error) {
	httpClient := webdav.HTTPClientWithBasicAuth(http.DefaultClient, p.Username, p.Password)
	c, err := cd.NewClient(httpClient, p.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("caldav: new client: %w", err)
	}
	return c, nil
}

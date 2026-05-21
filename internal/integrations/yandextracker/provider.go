// Package yandextracker — TrackerProvider для Yandex Tracker.
//
// REST API: https://tracker.yandex.net/v2/issues/_search
// Авторизация: OAuth-token (`Authorization: OAuth <token>`) + `X-Org-ID`.
package yandextracker

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"worktimesync/internal/integrations"
)

type Provider struct {
	httpClient *http.Client
}

func New() *Provider {
	return &Provider{httpClient: &http.Client{Timeout: 30 * time.Second}}
}

func (p *Provider) Name() integrations.Provider { return integrations.ProviderYandexTracker }

// AuthPayload — токен + ID организации.
type AuthPayload struct {
	OAuthToken string `json:"oauth_token"`
	OrgID      string `json:"org_id"`
	Username   string `json:"username"` // login или ID — для фильтрации задач по assignee
}

func (p *Provider) Authenticate(ctx context.Context, authCode string) (*integrations.Token, error) {
	if authCode == "" {
		return nil, errors.New("yandex_tracker: empty auth payload")
	}
	var payload AuthPayload
	if err := json.Unmarshal([]byte(authCode), &payload); err != nil {
		return nil, fmt.Errorf("yandex_tracker: parse: %w", err)
	}
	if payload.OAuthToken == "" || payload.OrgID == "" {
		return nil, errors.New("yandex_tracker: oauth_token/org_id required")
	}

	// Probe: GET /v2/myself
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		"https://api.tracker.yandex.net/v2/myself", nil)
	req.Header.Set("Authorization", "OAuth "+payload.OAuthToken)
	req.Header.Set("X-Org-ID", payload.OrgID)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex_tracker: probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("yandex_tracker: probe %d: %s", resp.StatusCode, string(body))
	}

	rawPayload, _ := json.Marshal(payload)
	return &integrations.Token{
		TokenType:   "oauth",
		AccessToken: payload.OAuthToken,
		Raw: map[string]any{
			"payload": string(rawPayload),
		},
	}, nil
}

func (p *Provider) RefreshToken(ctx context.Context, _ *integrations.Token) (*integrations.Token, error) {
	// Yandex OAuth токен живёт год+ — refresh не делаем на этом уровне.
	return nil, nil
}

// FetchTasks — задачи, назначенные на сотрудника.
func (p *Provider) FetchTasks(ctx context.Context, token *integrations.Token, assignee string, from, to time.Time) ([]integrations.Task, error) {
	if token == nil {
		return nil, errors.New("yandex_tracker: nil token")
	}
	payloadRaw, _ := token.Raw["payload"].(string)
	var payload AuthPayload
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return nil, err
	}

	who := assignee
	if who == "" {
		who = payload.Username
	}
	if who == "" {
		return nil, errors.New("yandex_tracker: assignee required")
	}

	// POST /v2/issues/_search с фильтром.
	body := map[string]any{
		"filter": map[string]any{
			"assignee": who,
			"due": map[string]string{
				"from": from.Format("2006-01-02"),
				"to":   to.Format("2006-01-02"),
			},
		},
	}
	raw, _ := json.Marshal(body)

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://api.tracker.yandex.net/v2/issues/_search?perPage=100",
		bytes.NewReader(raw))
	req.Header.Set("Authorization", "OAuth "+payload.OAuthToken)
	req.Header.Set("X-Org-ID", payload.OrgID)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("yandex_tracker: search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("yandex_tracker: search %d: %s", resp.StatusCode, string(body))
	}

	var out []struct {
		Key     string `json:"key"`
		Summary string `json:"summary"`
		Status  struct {
			Display string `json:"display"`
		} `json:"status"`
		Due       string `json:"due"`
		Estimation string `json:"estimation"` // ISO-8601 duration P1D, PT2H30M
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("yandex_tracker: decode: %w", err)
	}

	tasks := make([]integrations.Task, 0, len(out))
	for _, iss := range out {
		t := integrations.Task{
			SourceID: iss.Key,
			Title:    iss.Summary,
			Status:   iss.Status.Display,
		}
		if iss.Due != "" {
			if due, err := time.Parse(time.RFC3339, iss.Due); err == nil {
				t.DueAt = &due
			}
		}
		if iss.Estimation != "" {
			if est := parseISODurationHours(iss.Estimation); est > 0 {
				t.EstimatedHours = &est
			}
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

// parseISODurationHours — минимальный парсер ISO-8601 duration в часы.
// Поддерживаем P{n}D, PT{n}H, PT{n}M и комбинации (P1DT2H30M).
func parseISODurationHours(s string) float64 {
	hours := 0.0
	if len(s) < 2 || s[0] != 'P' {
		return 0
	}
	s = s[1:]
	timeIdx := -1
	for i := 0; i < len(s); i++ {
		if s[i] == 'T' {
			timeIdx = i
			break
		}
	}

	parseSection := func(section string, dayMode bool) float64 {
		var total float64
		num := 0
		for i := 0; i < len(section); i++ {
			ch := section[i]
			if ch >= '0' && ch <= '9' {
				num = num*10 + int(ch-'0')
				continue
			}
			switch ch {
			case 'D':
				total += float64(num) * 8 // считаем рабочий день = 8ч
			case 'H':
				total += float64(num)
			case 'M':
				if !dayMode {
					total += float64(num) / 60.0
				}
			}
			num = 0
		}
		return total
	}

	if timeIdx == -1 {
		hours += parseSection(s, true)
	} else {
		hours += parseSection(s[:timeIdx], true)
		hours += parseSection(s[timeIdx+1:], false)
	}
	return hours
}

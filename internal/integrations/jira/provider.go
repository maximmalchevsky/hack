package jira

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"worktimesync/internal/integrations"
)

type Provider struct {
	httpClient *http.Client
}

func New() *Provider {
	return &Provider{
		httpClient: &http.Client{Timeout: 30 * time.Second},
	}
}

func (p *Provider) Name() integrations.Provider { return integrations.ProviderJira }

type AuthPayload struct {
	BaseURL  string `json:"base_url"`
	Email    string `json:"email"`
	APIToken string `json:"api_token"`
}

func (p *Provider) Authenticate(ctx context.Context, authCode string) (*integrations.Token, error) {
	if authCode == "" {
		return nil, errors.New("jira: empty auth payload")
	}
	var payload AuthPayload
	if err := json.Unmarshal([]byte(authCode), &payload); err != nil {
		return nil, fmt.Errorf("jira: parse auth payload: %w", err)
	}
	if payload.BaseURL == "" || payload.Email == "" || payload.APIToken == "" {
		return nil, errors.New("jira: base_url/email/api_token required")
	}
	if _, err := url.Parse(payload.BaseURL); err != nil {
		return nil, fmt.Errorf("jira: invalid base_url: %w", err)
	}

	req, _ := http.NewRequestWithContext(ctx, http.MethodGet,
		strings.TrimRight(payload.BaseURL, "/")+"/rest/api/3/myself", nil)
	req.SetBasicAuth(payload.Email, payload.APIToken)
	req.Header.Set("Accept", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: probe: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira: probe failed: %d %s", resp.StatusCode, strings.TrimSpace(string(body)))
	}

	rawPayload, _ := json.Marshal(payload)
	return &integrations.Token{
		TokenType: "basic",
		Raw: map[string]any{
			"payload": string(rawPayload),
		},
	}, nil
}

func (p *Provider) RefreshToken(ctx context.Context, _ *integrations.Token) (*integrations.Token, error) {
	return nil, nil
}

func (p *Provider) FetchTasks(ctx context.Context, token *integrations.Token, assignee string, from, to time.Time) ([]integrations.Task, error) {
	if token == nil {
		return nil, errors.New("jira: nil token")
	}
	payloadRaw, _ := token.Raw["payload"].(string)
	var payload AuthPayload
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		return nil, err
	}

	jql := fmt.Sprintf(
		`assignee = "%s" AND (statusCategory != Done OR (duedate >= "%s" AND duedate <= "%s")) ORDER BY priority DESC, duedate ASC`,
		assignee,
		from.Format("2006-01-02"),
		to.Format("2006-01-02"),
	)
	body := map[string]any{
		"jql": jql,
		"fields": []string{
			"summary", "description", "status", "priority",
			"issuetype", "duedate", "timeoriginalestimate", "timespent",
		},
		"maxResults": 100,
	}
	bodyJSON, _ := json.Marshal(body)
	u := strings.TrimRight(payload.BaseURL, "/") + "/rest/api/3/search/jql"

	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, strings.NewReader(string(bodyJSON)))
	req.SetBasicAuth(payload.Email, payload.APIToken)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return nil, fmt.Errorf("jira: search: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("jira: search status %d: %s", resp.StatusCode, strings.TrimSpace(string(respBody)))
	}

	var out struct {
		Issues []struct {
			Key    string `json:"key"`
			Fields struct {
				Summary         string                 `json:"summary"`
				Description     any                    `json:"description"`
				Status          struct{ Name string }  `json:"status"`
				Priority        *struct{ Name string } `json:"priority"`
				IssueType       *struct{ Name string } `json:"issuetype"`
				DueDate         string                 `json:"duedate"`
				TimeOriginalSec int                    `json:"timeoriginalestimate"`
				TimeSpentSec    int                    `json:"timespent"`
			} `json:"fields"`
		} `json:"issues"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, fmt.Errorf("jira: decode: %w", err)
	}

	tasks := make([]integrations.Task, 0, len(out.Issues))
	for _, iss := range out.Issues {
		t := integrations.Task{
			SourceID:    iss.Key,
			Title:       iss.Fields.Summary,
			Description: flattenDescription(iss.Fields.Description),
			Status:      iss.Fields.Status.Name,
		}
		if iss.Fields.Priority != nil {
			t.Priority = iss.Fields.Priority.Name
		}
		if iss.Fields.IssueType != nil {
			t.Type = iss.Fields.IssueType.Name
		}
		if iss.Fields.DueDate != "" {
			if due, err := time.Parse("2006-01-02", iss.Fields.DueDate); err == nil {
				t.DueAt = &due
			}
		}
		if iss.Fields.TimeOriginalSec > 0 {
			est := float64(iss.Fields.TimeOriginalSec) / 3600.0
			t.EstimatedHours = &est
		} else if h, ok := ParseHoursFromText(t.Title + " " + t.Description); ok {
			est := h
			t.EstimatedHours = &est
		}
		if iss.Fields.TimeSpentSec > 0 {
			act := float64(iss.Fields.TimeSpentSec) / 3600.0
			t.ActualHours = &act
		}
		tasks = append(tasks, t)
	}
	return tasks, nil
}

func flattenDescription(d any) string {
	switch v := d.(type) {
	case nil:
		return ""
	case string:
		return strings.TrimSpace(v)
	case map[string]any:
		var sb strings.Builder
		walkADF(v, &sb)
		return strings.TrimSpace(sb.String())
	default:
		return ""
	}
}

func walkADF(node map[string]any, sb *strings.Builder) {
	if t, _ := node["type"].(string); t == "text" {
		if s, _ := node["text"].(string); s != "" {
			sb.WriteString(s)
			sb.WriteString(" ")
		}
	}
	if content, ok := node["content"].([]any); ok {
		for _, c := range content {
			if child, ok := c.(map[string]any); ok {
				walkADF(child, sb)
			}
		}
		if t, _ := node["type"].(string); t == "paragraph" || t == "heading" {
			sb.WriteString("\n")
		}
	}
}

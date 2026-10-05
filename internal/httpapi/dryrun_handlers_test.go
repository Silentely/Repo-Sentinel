package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestDryRun_NotificationSimulation(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{
		CSRFHeaderName: csrf.Value,
	}

	// Count existing outbox rows before dry run
	ctx := context.Background()
	initialOutbox, _, err := fixture.store.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	initialOutboxCount := len(initialOutbox)

	initialEvents, _, err := fixture.store.Events().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("failed to query events: %v", err)
	}
	initialEventsCount := len(initialEvents)

	initialAudits, err := fixture.store.Audits().List(ctx, 1, 100)
	if err != nil {
		t.Fatalf("failed to query audits: %v", err)
	}
	initialAuditsCount := len(initialAudits)

	dryRunReq := map[string]any{
		"event_type": "pull_request",
		"action":     "opened",
		"repository": "Silentely/Repo-Sentinel",
		"branch":     "main",
		"payload_raw": `{
			"action": "opened",
			"repository": {"full_name": "Silentely/Repo-Sentinel", "html_url": "https://github.com/Silentely/Repo-Sentinel"},
			"pull_request": {
				"number": 42,
				"title": "feat: notification simulator",
				"html_url": "https://github.com/Silentely/Repo-Sentinel/pull/42",
				"user": {"login": "octocat"},
				"head": {"ref": "feat-simulator"},
				"base": {"ref": "main"}
			},
			"sender": {"login": "octocat"}
		}`,
		"channels": []map[string]any{
			{
				"id":           "chan-sim-1",
				"channel_type": "telegram",
				"type":         "telegram",
				"name":         "Dev Telegram",
				"enabled":      true,
				"event_kinds":  []string{"pull_request", "issue"},
			},
			{
				"id":           "chan-sim-2",
				"channel_type": "feishu",
				"type":         "feishu",
				"name":         "Ops Feishu",
				"enabled":      false,
				"event_kinds":  []string{"release"},
			},
		},
	}

	bodyBytes, _ := json.Marshal(dryRunReq)
	rec := fixture.request(t, http.MethodPost, "/api/v1/rules/dry-run", string(bodyBytes), "127.0.0.1:45100", cookies, headers)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK from dry-run, got %d: %s", rec.Code, rec.Body.String())
	}

	var resp struct {
		EventKind      string   `json:"event_kind"`
		Action         string   `json:"action"`
		Repository     string   `json:"repository"`
		Title          string   `json:"title"`
		BodyText       string   `json:"body_text"`
		HTMLURL        string   `json:"html_url"`
		MatchedRules   []string `json:"matched_rules"`
		IsMuted        bool     `json:"is_muted"`
		ChannelResults []struct {
			ChannelID   string `json:"channel_id"`
			ChannelType string `json:"channel_type"`
			Name        string `json:"name"`
			Matched     bool   `json:"matched"`
			Reason      string `json:"reason"`
			ParseMode   string `json:"parse_mode"`
		} `json:"channel_results"`
	}

	if err := json.Unmarshal(rec.Body.Bytes(), &resp); err != nil {
		t.Fatalf("unmarshal response: %v", err)
	}

	if resp.Repository != "Silentely/Repo-Sentinel" {
		t.Errorf("expected repo Silentely/Repo-Sentinel, got %s", resp.Repository)
	}
	if resp.Title == "" {
		t.Errorf("expected non-empty title")
	}
	if resp.BodyText == "" {
		t.Errorf("expected non-empty body_text")
	}
	if len(resp.ChannelResults) != 2 {
		t.Fatalf("expected 2 channel results, got %d", len(resp.ChannelResults))
	}

	// chan-sim-1 should match
	if !resp.ChannelResults[0].Matched {
		t.Errorf("expected chan-sim-1 to match pull_request event")
	}
	// chan-sim-2 is disabled & not subscribed to pull_request
	if resp.ChannelResults[1].Matched {
		t.Errorf("expected chan-sim-2 to NOT match")
	}

	// Verify Outbox is 100% unchanged (Zero side effects)
	afterOutbox, _, err := fixture.store.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
	if err != nil {
		t.Fatalf("failed to query outbox: %v", err)
	}
	if len(afterOutbox) != initialOutboxCount {
		t.Fatalf("dry run must NOT create outbox records, count before=%d, after=%d", initialOutboxCount, len(afterOutbox))
	}
	afterEvents, _, _ := fixture.store.Events().List(ctx, store.ListFilter{Page: 1, PerPage: 100})
	if len(afterEvents) != initialEventsCount {
		t.Fatalf("dry run must NOT create events records, before=%d, after=%d", initialEventsCount, len(afterEvents))
	}
	afterAudits, _ := fixture.store.Audits().List(ctx, 1, 100)
	if len(afterAudits) != initialAuditsCount {
		t.Fatalf("dry run must NOT create audit records, before=%d, after=%d", initialAuditsCount, len(afterAudits))
	}
}

func TestDryRun_UnauthenticatedReturns401(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	rec := fixture.request(t, http.MethodPost, "/api/v1/rules/dry-run", `{"event_type":"pull_request"}`, "127.0.0.1:45100", nil, nil)
	assertAPIError(t, rec, http.StatusUnauthorized, errorCodeUnauthorized)
}

func TestDryRun_ValidationFailed(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	// Missing event_type
	rec := fixture.request(t, http.MethodPost, "/api/v1/rules/dry-run", `{"event_type":""}`, "127.0.0.1:45100", cookies, headers)
	assertAPIError(t, rec, http.StatusBadRequest, errorCodeValidationFailed)
}

package rules

import (
	"context"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestMatchChannelFilter_RepoPattern(t *testing.T) {
	tests := []struct {
		name      string
		pattern   string
		repoName  string
		wantMatch bool
	}{
		{
			name:      "empty pattern matches all",
			pattern:   "",
			repoName:  "Silentely/Repo-Sentinel",
			wantMatch: true,
		},
		{
			name:      "wildcard owner match",
			pattern:   "Silentely/*",
			repoName:  "Silentely/Repo-Sentinel",
			wantMatch: true,
		},
		{
			name:      "wildcard owner mismatch",
			pattern:   "Silentely/*",
			repoName:  "OtherOrg/Repo-Sentinel",
			wantMatch: false,
		},
		{
			name:      "negative pattern takes precedence",
			pattern:   "Silentely/*, !*-archive",
			repoName:  "Silentely/old-archive",
			wantMatch: false,
		},
		{
			name:      "negative pattern non-matching passes",
			pattern:   "Silentely/*, !*-archive",
			repoName:  "Silentely/active-repo",
			wantMatch: true,
		},
		{
			name:      "only negative patterns",
			pattern:   "!*-archive, !test/*",
			repoName:  "Silentely/my-repo",
			wantMatch: true,
		},
		{
			name:      "only negative patterns rejected",
			pattern:   "!*-archive, !test/*",
			repoName:  "test/demo",
			wantMatch: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := store.NotificationChannel{
				RepoPattern: tt.pattern,
				MinSeverity: "low",
			}
			got := MatchChannelFilter(ch, &store.Event{}, tt.repoName, "")
			if got != tt.wantMatch {
				t.Errorf("MatchChannelFilter() = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestMatchChannelFilter_BranchFilter(t *testing.T) {
	tests := []struct {
		name      string
		filter    string
		branch    string
		wantMatch bool
	}{
		{
			name:      "empty filter matches any branch",
			filter:    "",
			branch:    "feature/login",
			wantMatch: true,
		},
		{
			name:      "exact match",
			filter:    "main, master",
			branch:    "main",
			wantMatch: true,
		},
		{
			name:      "wildcard branch match",
			filter:    "release/*, main",
			branch:    "release/v1.0",
			wantMatch: true,
		},
		{
			name:      "branch mismatch",
			filter:    "release/*, main",
			branch:    "feature/new-button",
			wantMatch: false,
		},
		{
			name:      "empty branch for non-branch event passes",
			filter:    "main",
			branch:    "",
			wantMatch: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := store.NotificationChannel{
				BranchFilter: tt.filter,
				MinSeverity:  "low",
			}
			got := MatchChannelFilter(ch, &store.Event{}, "Silentely/Repo-Sentinel", tt.branch)
			if got != tt.wantMatch {
				t.Errorf("MatchChannelFilter() = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestMatchChannelFilter_MinSeverity(t *testing.T) {
	tests := []struct {
		name        string
		minSeverity string
		eventSev    string
		wantMatch   bool
	}{
		{
			name:        "equal severity matches",
			minSeverity: "high",
			eventSev:    "high",
			wantMatch:   true,
		},
		{
			name:        "higher severity matches",
			minSeverity: "medium",
			eventSev:    "critical",
			wantMatch:   true,
		},
		{
			name:        "lower severity rejected",
			minSeverity: "high",
			eventSev:    "low",
			wantMatch:   false,
		},
		{
			name:        "unknown severity falls back to low safely",
			minSeverity: "medium",
			eventSev:    "unknown_foo",
			wantMatch:   false, // unknown falls back to low (1), min medium (2) -> false
		},
		{
			name:        "unknown severity with low threshold matches",
			minSeverity: "low",
			eventSev:    "unknown_foo",
			wantMatch:   true,
		},
		{
			name:        "empty event severity passes",
			minSeverity: "high",
			eventSev:    "",
			wantMatch:   true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ch := store.NotificationChannel{
				MinSeverity: tt.minSeverity,
			}
			ev := &store.Event{Severity: tt.eventSev}
			got := MatchChannelFilter(ch, ev, "Silentely/Repo-Sentinel", "")
			if got != tt.wantMatch {
				t.Errorf("MatchChannelFilter() severity got = %v, want %v", got, tt.wantMatch)
			}
		})
	}
}

func TestPersistSuppressedNotification_EmergencyMute(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()

	ch, err := st.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          "ch-tg-mute-test",
		ChannelType: store.ChannelTelegram,
		Name:        "Test Channel",
		Enabled:     true,
		Target:      "-100123456",
		MinSeverity: "low",
	})
	if err != nil {
		t.Fatalf("create channel failed: %v", err)
	}

	// 激活紧急静音 (独立设置键命名空间: mute:repo:xxx)
	repoID := "repo-mute-1"
	muteKey := store.MuteSettingKey(repoID)
	_, err = st.Settings().Upsert(ctx, store.SystemSetting{
		ID:        "set-mute-1",
		Key:       muteKey,
		ValueJSON: []byte(`{"muted":true,"reason":"emergency_mute","expires_at":"2099-01-01T00:00:00Z"}`),
		UpdatedAt: time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("upsert mute setting failed: %v", err)
	}

	engine := &Engine{Store: st}
	ev := &store.Event{
		ID:           "ev-muted-1",
		Kind:         store.WorkItemKindIssue,
		Action:       "opened",
		RepositoryID: &repoID,
		Title:        "Critical outage test",
		OccurredAt:   time.Now().UTC(),
	}

	res := normalizer.Result{
		Event: ev,
		Repository: &store.Repository{
			ID:             repoID,
			FullName:       "Silentely/critical-app",
			MonitorEnabled: true,
			IssuesEnabled:  true,
		},
	}

	err = engine.Evaluate(ctx, res, "Silentely/critical-app")
	if err != nil {
		t.Fatalf("Evaluate() error = %v", err)
	}

	// 验证未产生发送状态的 outbox，而是记录了 suppressed 审计行
	items, _, err := st.Outbox().List(ctx, store.ListFilter{ChannelIDs: []string{ch.ID}})
	if err != nil {
		t.Fatalf("list outbox error: %v", err)
	}

	var foundSuppressed bool
	for _, it := range items {
		if it.Status == store.OutboxSuppressed {
			foundSuppressed = true
			if it.SuppressedReason != "emergency_mute" {
				t.Errorf("SuppressedReason = %q, want %q", it.SuppressedReason, "emergency_mute")
			}
			if it.IdempotencyKey != "suppressed|ev-muted-1|ch-tg-mute-test" {
				t.Errorf("IdempotencyKey = %q, want suppressed namespace key", it.IdempotencyKey)
			}
		}
		if it.Status == store.OutboxPending || it.Status == store.OutboxSending {
			t.Errorf("unexpected active outbox item found during emergency mute: %+v", it)
		}
	}

	if !foundSuppressed {
		t.Fatalf("expected suppressed outbox audit record was not found")
	}
}

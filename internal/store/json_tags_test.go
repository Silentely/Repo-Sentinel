package store

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

// 防止 API 再次把领域模型以 PascalCase 输出，导致前端 full_name / title 等读不到。
func Test领域模型JSON使用蛇形命名(t *testing.T) {
	cases := []struct {
		name string
		v    any
		want []string
		deny []string
	}{
		{
			name: "Repository",
			v: Repository{
				ID: "r1", Type: RepositoryTypeInstallation, SyncStatus: SyncStatusBaseline,
				Owner: "Silentely", Name: "Demo", FullName: "Silentely/Demo", HTMLURL: "https://github.com/Silentely/Demo",
			},
			want: []string{`"id"`, `"full_name":"Silentely/Demo"`, `"sync_status":"baseline_sync"`, `"html_url"`, `"owner"`, `"name"`},
			deny: []string{`"FullName"`, `"SyncStatus"`},
		},
		{
			name: "GitHubInstallation",
			v:    GitHubInstallation{ID: "i1", InstallationID: 149631800, AccountLogin: "Silentely", AccountType: "User", Suspended: "false"},
			want: []string{`"installation_id":149631800`, `"account_login":"Silentely"`, `"account_type":"User"`, `"suspended"`},
			deny: []string{`"AccountLogin"`, `"InstallationID"`},
		},
		{
			name: "WorkItem",
			v: WorkItem{
				ID: "w1", RepositoryID: "r1", Number: 7, Kind: WorkItemKindIssue, State: "open",
				Title: "hello world", Author: "alice", HTMLURL: "https://github.com/a/b/issues/7",
				SourceUpdatedAt: time.Now().UTC(),
			},
			want: []string{`"number":7`, `"title":"hello world"`, `"kind":"issue"`, `"state":"open"`, `"html_url"`, `"author"`},
			deny: []string{`"Number"`, `"Title"`, `"HTMLURL"`},
		},
		{
			name: "WorkflowRun",
			v: WorkflowRun{
				ID: "wr1", RepositoryID: "r1", GitHubRunID: 99, WorkflowName: "CI", RunNumber: 3,
				Status: "completed", HeadBranch: "main", HTMLURL: "https://github.com/a/b/actions/runs/99",
				RunUpdatedAt: time.Now().UTC(),
			},
			want: []string{`"workflow_name":"CI"`, `"run_number":3`, `"status":"completed"`, `"head_branch":"main"`, `"html_url"`},
			deny: []string{`"WorkflowName"`, `"RunNumber"`},
		},
		{
			name: "SecurityAlert",
			v: SecurityAlert{
				ID: "s1", RepositoryID: "r1", AlertKind: AlertKindDependabot, AlertNumber: 2,
				State: "open", Severity: "high", RuleOrDependency: "lodash", HTMLURL: "https://github.com/a/b/security",
				SourceUpdatedAt: time.Now().UTC(),
			},
			want: []string{`"alert_kind":"dependabot"`, `"alert_number":2`, `"rule_or_dependency":"lodash"`, `"severity":"high"`, `"html_url"`},
			deny: []string{`"AlertKind"`, `"AlertNumber"`, `"RuleOrDependency"`},
		},
		{
			name: "Event",
			v: Event{
				ID: "e1", Source: "webhook", Kind: "issue", Action: "opened", Title: "t", Severity: "info",
				HTMLURL: "https://github.com/a/b", OccurredAt: time.Now().UTC(),
			},
			want: []string{`"kind":"issue"`, `"action":"opened"`, `"title":"t"`, `"html_url"`, `"suppress_notification"`},
			deny: []string{`"Kind"`, `"HTMLURL"`},
		},
		{
			name: "NotificationOutbox",
			v: NotificationOutbox{
				ID: "o1", ChannelID: "c1", Status: OutboxDead, AttemptCount: 3, Title: "alert",
				LastErrorCode: "delivery_failed", NextAttemptAt: time.Now().UTC(),
			},
			want: []string{`"status":"dead"`, `"attempt_count":3`, `"title":"alert"`, `"last_error_code":"delivery_failed"`},
			deny: []string{`"AttemptCount"`, `"LastErrorCode"`},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			raw, err := json.Marshal(tc.v)
			if err != nil {
				t.Fatal(err)
			}
			s := string(raw)
			for _, want := range tc.want {
				if !strings.Contains(s, want) {
					t.Fatalf("缺少 %s\n实际=%s", want, s)
				}
			}
			for _, deny := range tc.deny {
				if strings.Contains(s, deny) {
					t.Fatalf("不应出现 %s\n实际=%s", deny, s)
				}
			}
		})
	}
}

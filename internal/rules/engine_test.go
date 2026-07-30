package rules

import (
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestRenderMessage_IssueOpened(t *testing.T) {
	now := time.Date(2026, 7, 30, 9, 15, 0, 0, time.UTC)
	num := 8
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "[BUG] 脚本一直在跑",
		Actor: "testuser", SubjectNumber: &num, OccurredAt: now, HTMLURL: "https://github.com/test/repo/issues/8",
		PayloadSummary: map[string]any{"state": "open", "draft": false, "labels": []any{"bug", "urgent"}, "assignees": []any{"user1"}, "milestone": "v1.0"},
	}
	title, body, htmlURL := renderMessage(ev, "Silentely/xserver-vps-renew")

	if !strings.Contains(title, "🐛") {
		t.Errorf("期望 🐛 emoji，标题: %s", title)
	}
	if !strings.Contains(body, "Silentely/xserver-vps-renew") {
		t.Error("期望包含仓库名")
	}
	if !strings.Contains(body, "#8") {
		t.Error("期望包含编号")
	}
	if !strings.Contains(body, "bug, urgent") {
		t.Errorf("期望包含标签，实际: %s", body)
	}
	if !strings.Contains(body, "user1") {
		t.Errorf("期望包含指派人，实际: %s", body)
	}
	if !strings.Contains(body, "v1.0") {
		t.Errorf("期望包含里程碑，实际: %s", body)
	}
	if !strings.Contains(body, "2026-07-30 09:15 UTC") {
		t.Errorf("期望包含时间，实际: %s", body)
	}
	if htmlURL != "https://github.com/test/repo/issues/8" {
		t.Errorf("期望返回 htmlURL，实际: %s", htmlURL)
	}
}

func TestRenderMessage_PRMerged(t *testing.T) {
	num := 42
	ev := &store.Event{
		Kind: store.WorkItemKindPR, Action: "merged", Title: "feat: 新功能",
		Actor: "dev", SubjectNumber: &num, OccurredAt: time.Now().UTC(), HTMLURL: "https://github.com/test/repo/pull/42",
		PayloadSummary: map[string]any{"state": "closed", "draft": false},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🟣") {
		t.Errorf("期望 🟣 emoji 表示 merged，标题: %s", title)
	}
	if !strings.Contains(body, "已合并") {
		t.Errorf("期望包含「已合并」状态，实际: %s", body)
	}
}

func TestRenderMessage_PRDraft(t *testing.T) {
	num := 50
	ev := &store.Event{
		Kind: store.WorkItemKindPR, Action: "opened", Title: "WIP: draft pr",
		SubjectNumber: &num, OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "draft": true},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "📝") {
		t.Errorf("期望 📝 emoji 表示 draft，标题: %s", title)
	}
	if !strings.Contains(body, "草稿") {
		t.Errorf("期望包含「草稿」状态，实际: %s", body)
	}
}

func TestRenderMessage_SecurityAlert_Critical(t *testing.T) {
	ev := &store.Event{
		Kind: store.AlertKindDependabot, Action: "opened", Title: "lodash prototype pollution",
		Severity: "critical", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "severity": "critical", "rule_or_dependency": "lodash"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🚨") {
		t.Errorf("期望 🚨 emoji 表示 critical，标题: %s", title)
	}
	if !strings.Contains(body, "🚨 严重度：critical") {
		t.Errorf("期望 🚨 严重度前缀，实际: %s", body)
	}
	if !strings.Contains(body, "lodash") {
		t.Errorf("期望包含规则名，实际: %s", body)
	}
}

func TestRenderMessage_CodeScanning_Error(t *testing.T) {
	ev := &store.Event{
		Kind: store.AlertKindCodeScanning, Action: "opened", Title: "SQL injection",
		Severity: "error", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"state": "open", "severity": "error", "rule_or_dependency": "sql-injection"},
	}
	title, _, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🔴") {
		t.Errorf("期望 🔴 emoji 表示 error 级别 code_scanning，标题: %s", title)
	}
}

func TestRenderMessage_WorkflowFailure(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "completed", Title: "CI",
		WorkflowConclusion: "failure", Actor: "bot", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"head_branch": "main", "workflow_name": "CI Pipeline"},
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "❌") {
		t.Errorf("期望 ❌ emoji 表示 failure，标题: %s", title)
	}
	if !strings.Contains(body, "main") {
		t.Errorf("期望包含分支名，实际: %s", body)
	}
	if !strings.Contains(body, "CI Pipeline") {
		t.Errorf("期望包含工作流名，实际: %s", body)
	}
}

func TestRenderMessage_WorkflowRecovered(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "recovered", Title: "CI",
		WorkflowConclusion: "success", OccurredAt: time.Now().UTC(),
		PayloadSummary: map[string]any{"head_branch": "main"},
	}
	title, _, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "🟢") {
		t.Errorf("期望 🟢 emoji 表示 recovered，标题: %s", title)
	}
}

func TestRenderMessage_WorkflowCancelled(t *testing.T) {
	ev := &store.Event{
		Kind: "workflow_run", Action: "completed", Title: "Deploy",
		WorkflowConclusion: "cancelled", OccurredAt: time.Now().UTC(),
	}
	title, body, _ := renderMessage(ev, "test/repo")

	if !strings.Contains(title, "⏹️") {
		t.Errorf("期望 ⏹️ emoji 表示 cancelled，标题: %s", title)
	}
	if !strings.Contains(body, "⏹️ 结论：cancelled") {
		t.Errorf("期望 ⏹️ 结论前缀，实际: %s", body)
	}
}

func TestRenderMessage_NoRepo(t *testing.T) {
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "test",
		OccurredAt: time.Now().UTC(),
	}
	_, body, _ := renderMessage(ev, "")

	if strings.Contains(body, "仓库") {
		t.Errorf("无仓库时不应包含仓库字段，实际: %s", body)
	}
}

func TestRenderMessage_NoHTMLURL(t *testing.T) {
	ev := &store.Event{
		Kind: store.WorkItemKindIssue, Action: "opened", Title: "test",
		OccurredAt: time.Now().UTC(), HTMLURL: "",
	}
	_, body, htmlURL := renderMessage(ev, "test/repo")

	if htmlURL != "" {
		t.Errorf("无链接时 htmlURL 应为空，实际: %s", htmlURL)
	}
	if strings.Contains(body, "在 GitHub 中查看") {
		t.Errorf("无链接时不应包含查看链接，实际: %s", body)
	}
}

func TestEventEmoji_SecretScanning(t *testing.T) {
	ev := &store.Event{Kind: store.AlertKindSecretScanning, Action: "opened"}
	if got := eventEmoji(ev); got != "🔑" {
		t.Errorf("期望 🔑，实际: %s", got)
	}
}

func TestSeverityEmoji_All(t *testing.T) {
	cases := []struct {
		severity, want string
	}{
		{"critical", "🚨"}, {"high", "🔴"}, {"error", "🔴"},
		{"medium", "🟠"}, {"warning", "🟠"}, {"low", "🟡"}, {"note", "🟡"},
		{"unknown", "⚠️"}, {"", "⚠️"},
	}
	for _, tc := range cases {
		if got := severityEmoji(tc.severity); got != tc.want {
			t.Errorf("severity=%q: 期望 %s，实际 %s", tc.severity, tc.want, got)
		}
	}
}

func TestWorkflowConclusionEmoji_All(t *testing.T) {
	cases := []struct {
		conclusion, want string
	}{
		{"success", "✅"}, {"failure", "❌"}, {"cancelled", "⏹️"},
		{"timed_out", "⏱️"}, {"action_required", "🔔"}, {"skipped", "⏭️"},
		{"startup_failure", "💥"}, {"unknown", "📊"},
	}
	for _, tc := range cases {
		if got := workflowConclusionEmoji(tc.conclusion); got != tc.want {
			t.Errorf("conclusion=%q: 期望 %s，实际 %s", tc.conclusion, tc.want, got)
		}
	}
}

func TestPayloadString(t *testing.T) {
	m := map[string]any{"key": "value", "num": 42}
	if got := payloadString(m, "key"); got != "value" {
		t.Errorf("期望 value，实际: %s", got)
	}
	if got := payloadString(m, "num"); got != "" {
		t.Errorf("非字符串应返回空，实际: %s", got)
	}
	if got := payloadString(nil, "key"); got != "" {
		t.Errorf("nil map 应返回空，实际: %s", got)
	}
	if got := payloadString(m, "missing"); got != "" {
		t.Errorf("缺失 key 应返回空，实际: %s", got)
	}
}

func TestPayloadStringSlice(t *testing.T) {
	m := map[string]any{"labels": []any{"a", "b"}, "bad": []any{1, 2}}
	labels := payloadStringSlice(m, "labels")
	if len(labels) != 2 || labels[0] != "a" || labels[1] != "b" {
		t.Errorf("期望 [a b]，实际: %v", labels)
	}
	if got := payloadStringSlice(m, "bad"); len(got) != 0 {
		t.Errorf("非字符串元素应返回空切片，实际: %v", got)
	}
	if got := payloadStringSlice(nil, "key"); got != nil {
		t.Errorf("nil map 应返回 nil，实际: %v", got)
	}
}

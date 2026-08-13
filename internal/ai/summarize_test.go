package ai

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// stubClient 返回指向桩服务器的客户端，并捕获请求体。
func stubClient(t *testing.T, reply string) (*Client, func() chatRequest) {
	t.Helper()
	var got chatRequest
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(raw, &got)
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(reply))
	}))
	t.Cleanup(srv.Close)
	return &Client{BaseURL: srv.URL, APIKey: "k", Enabled: true}, func() chatRequest { return got }
}

func TestSummarizeEvents(t *testing.T) {
	client, capture := stubClient(t, `{"choices":[{"message":{"content":"今日共 3 条事件：- 依赖告警升级 lodash"}}]}`)
	num := int64(7)
	events := []store.Event{
		{Kind: store.AlertKindDependabot, Title: "lodash < 4.17.21 存在原型污染", Severity: "high",
			SubjectNumber: &num, RepositoryID: strPtr("repo-1"),
			PayloadSummary: map[string]any{"rule_or_dependency": "lodash"}},
	}
	repoNames := map[string]string{"repo-1": "acme/web"}

	out, err := client.SummarizeEvents(t.Context(), events, repoNames, "过去 24 小时")
	if err != nil {
		t.Fatalf("SummarizeEvents 应成功: %v", err)
	}
	if !strings.Contains(out, "lodash") {
		t.Fatalf("期望包含告警信息，实际: %q", out)
	}

	req := capture()
	if len(req.Messages) != 2 {
		t.Fatalf("期望 system/user 双消息，实际 %d 条", len(req.Messages))
	}
	user := req.Messages[1].Content
	for _, want := range []string{"过去 24 小时", "lodash", "acme/web", "#7", "严重度:high"} {
		if !strings.Contains(user, want) {
			t.Errorf("用户消息应包含 %q，实际: %s", want, user)
		}
	}
	if !strings.Contains(req.Messages[0].Content, "报告摘要器") {
		t.Errorf("系统消息应为摘要器 prompt，实际: %s", req.Messages[0].Content)
	}
}

func TestSummarizeEventsEmpty(t *testing.T) {
	client, _ := stubClient(t, `{"choices":[{"message":{"content":"x"}}]}`)
	out, err := client.SummarizeEvents(t.Context(), nil, nil, "过去 24 小时")
	if err != nil || out != "" {
		t.Fatalf("空事件应直接返回空串，实际 out=%q err=%v", out, err)
	}
}

func TestReleaseSummary(t *testing.T) {
	client, capture := stubClient(t, `{"choices":[{"message":{"content":"新增 X 功能，修复 Y；⚠️ 破坏性变更：Z"}}]}`)
	out, err := client.ReleaseSummary(t.Context(), "octocat/Hello-World", "v2.0.0", "Some English notes", "https://github.com/o/r/releases/tag/v2.0.0")
	if err != nil {
		t.Fatalf("ReleaseSummary 应成功: %v", err)
	}
	if !strings.Contains(out, "X 功能") {
		t.Fatalf("期望包含总结内容，实际: %q", out)
	}
	req := capture()
	if len(req.Messages) != 2 {
		t.Fatalf("期望 system/user 双消息，实际 %d 条", len(req.Messages))
	}
	user := req.Messages[1].Content
	for _, want := range []string{"octocat/Hello-World", "v2.0.0", "Some English notes", "https://github.com/o/r/releases/tag/v2.0.0"} {
		if !strings.Contains(user, want) {
			t.Errorf("用户消息应包含 %q，实际: %s", want, user)
		}
	}
	if !strings.Contains(req.Messages[0].Content, "发布说明摘要器") {
		t.Errorf("系统消息应为发布说明摘要 prompt，实际: %s", req.Messages[0].Content)
	}
}

func TestReleaseSummaryTruncatesLongNotes(t *testing.T) {
	client, capture := stubClient(t, `{"choices":[{"message":{"content":"ok"}}]}`)
	long := strings.Repeat("x", 12000)
	if _, err := client.ReleaseSummary(t.Context(), "o/r", "v1", long, ""); err != nil {
		t.Fatal(err)
	}
	req := capture()
	if len(req.Messages[1].Content) > maxReleaseNotesChars+100 {
		t.Fatalf("notes 应被截断到上限附近，实际 %d 字符", len(req.Messages[1].Content))
	}
}

func TestReleaseSummaryNotEnabled(t *testing.T) {
	c := &Client{Enabled: true, APIKey: "k", DigestEnabled: true}
	if c.IsReleaseSummaryEnabled() {
		t.Fatal("ReleaseSummaryEnabled 缺省 false 时应不可用")
	}
	c2 := &Client{Enabled: true, APIKey: "k", ReleaseSummaryEnabled: true}
	if !c2.IsReleaseSummaryEnabled() {
		t.Fatal("ReleaseSummaryEnabled true 时应可用")
	}
}

func TestSummarizeEventsError(t *testing.T) {
	client, _ := stubClient(t, `not-json`)
	if _, err := client.SummarizeEvents(t.Context(), []store.Event{{Kind: store.WorkItemKindIssue, Title: "t"}}, nil, "过去 24 小时"); err == nil {
		t.Fatal("期望响应解析失败时返回错误")
	}
}

func TestTriageAlert(t *testing.T) {
	client, capture := stubClient(t, `{"choices":[{"message":{"content":"影响：攻击者可利用。\n建议：升级到 4.17.21。"}}]}`)
	num := int64(3)
	ev := store.Event{
		Kind: store.AlertKindDependabot, Title: "lodash 漏洞", Severity: "critical", SubjectNumber: &num,
		HTMLURL:        "https://github.com/acme/web/security/dependabot/3",
		PayloadSummary: map[string]any{"rule_or_dependency": "lodash"},
	}
	out, err := client.TriageAlert(t.Context(), ev, "acme/web")
	if err != nil {
		t.Fatalf("TriageAlert 应成功: %v", err)
	}
	if !strings.Contains(out, "影响：") || !strings.Contains(out, "建议：") {
		t.Fatalf("期望影响/建议两段式输出，实际: %q", out)
	}

	req := capture()
	user := req.Messages[1].Content
	for _, want := range []string{"Dependabot 依赖告警", "acme/web", "lodash 漏洞", "critical", "lodash"} {
		if !strings.Contains(user, want) {
			t.Errorf("用户消息应包含 %q，实际: %s", want, user)
		}
	}
	if !strings.Contains(req.Messages[0].Content, "安全分析助手") {
		t.Errorf("系统消息应为安全分析 prompt，实际: %s", req.Messages[0].Content)
	}
}

func TestRenderEventLinesTruncates(t *testing.T) {
	events := make([]store.Event, maxEventLines+10)
	for i := range events {
		events[i] = store.Event{Kind: store.WorkItemKindIssue, Title: "t"}
	}
	lines := renderEventLines(events, nil)
	if !strings.Contains(lines, "另有 10 条未列出") {
		t.Fatalf("期望截断提示，实际: %s", lines)
	}
}

// TestRenderEventLines_ReleaseRepoFallback 验证 star 追踪的 release 事件（无 RepositoryID）
// 经 PayloadSummary 回退补仓库名，杜绝 AI 面对无主 release 猜测归属（曾误归到 eSIM-Tools）。
func TestRenderEventLines_ReleaseRepoFallback(t *testing.T) {
	events := []store.Event{
		{Kind: store.ReleaseKind, Title: "v0.4.24 - xAI SuperGrok plan_type hotfix",
			SubjectNumber: intPtr(369624730),
			PayloadSummary: map[string]any{
				"tag_name": "v0.4.24", "repository": "kittors/CliRelay",
			}},
	}
	lines := renderEventLines(events, nil)
	if !strings.Contains(lines, "kittors/CliRelay") {
		t.Fatalf("release 事件应回退补仓库名，实际: %s", lines)
	}
	if strings.Contains(lines, "（）") {
		t.Fatalf("不应出现空括号，实际: %s", lines)
	}
}

// TestRenderEventLines_StarTitleDedup 验证 star/watch 事件标题即仓库名时不再追加「（名）」重复，
// 避免「Silentely/eSIM-Tools（Silentely/eSIM-Tools）」式噪声行污染 AI 输入。
func TestRenderEventLines_StarTitleDedup(t *testing.T) {
	repoID := "repo-1"
	events := []store.Event{
		{Kind: store.StarKind, Title: "acme/demo", RepositoryID: &repoID},
		{Kind: store.WorkItemKindIssue, Title: "修复登录 Bug", RepositoryID: &repoID},
	}
	lines := renderEventLines(events, map[string]string{"repo-1": "acme/demo"})
	if strings.Contains(lines, "acme/demo（acme/demo）") {
		t.Fatalf("标题即仓库名时不应重复追加，实际: %s", lines)
	}
	if !strings.Contains(lines, "修复登录 Bug（acme/demo）") {
		t.Fatalf("普通事件应带仓库名，实际: %s", lines)
	}
}

func strPtr(s string) *string { return &s }

func intPtr(v int64) *int64 { return &v }

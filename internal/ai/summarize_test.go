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
	client := &Client{Enabled: true}
	out, err := client.SummarizeEvents(t.Context(), nil, nil, "过去 24 小时")
	if err != nil {
		t.Fatalf("空事件不应报错: %v", err)
	}
	if out != "" {
		t.Fatalf("空事件期望空输出，实际: %q", out)
	}
}

func TestRenderEventLines_StatusTags(t *testing.T) {
	num1, num2 := int64(10), int64(13)
	repoID := "repo-1"
	events := []store.Event{
		{
			Kind: store.AlertKindDependabot, Action: "fixed", Title: "source-map-js",
			SubjectNumber: &num1, RepositoryID: &repoID, Severity: "high",
		},
		{
			Kind: store.WorkItemKindIssue, Action: "closed", Title: "fix bug",
			SubjectNumber: &num2, RepositoryID: &repoID,
		},
		{
			Kind: store.WorkflowRunKind, Action: "recovered", Title: "CI Build",
		},
	}
	names := map[string]string{"repo-1": "Silentely/test-repo"}
	lines := renderEventLines(events, names)

	if !strings.Contains(lines, "[状态:已修复]") {
		t.Fatalf("安全告警 fixed 应渲染 [状态:已修复]，实际:\n%s", lines)
	}
	if !strings.Contains(lines, "[状态:已关闭]") {
		t.Fatalf("Issue closed 应渲染 [状态:已关闭]，实际:\n%s", lines)
	}
	if !strings.Contains(lines, "[状态:已恢复]") {
		t.Fatalf("Workflow recovered 应渲染 [状态:已恢复]，实际:\n%s", lines)
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

func strPtr(s string) *string {
	return &s
}

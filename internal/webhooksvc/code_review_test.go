package webhooksvc_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
)

func TestServiceAICodeReviewFlow(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	ctx := t.Context()

	var commentPosted atomic.Bool

	// 模拟 GitHub API 与 AI 接口
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/42"):
			// Diff 接口
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("diff --git a/main.go b/main.go\n+func test() {}\n"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/42/comments"):
			// 发评接口
			commentPosted.Store(true)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 123}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			// OpenAI 兼容接口
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			res := map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": `{"summary":"PR代码良好","score":92,"security_risks":[],"breaking_risks":[],"code_smells":["建议补充注释"]}`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer fakeServer.Close()

	// 构造 AI 客户端并启用 PR 审查与回写
	aiClient := &ai.Client{
		Enabled:               true,
		BaseURL:               fakeServer.URL,
		Model:                 "mock-model",
		APIKey:                "mock-key",
		CodeReviewEnabled:     true,
		CodeReviewCommentOnPR: true,
	}

	// 构造 GitHub AppClient 指向 mock 端点
	ghClient := githubx.NewAppClient(1234, "")
	ghClient.BaseURL = fakeServer.URL

	svc := &webhooksvc.Service{
		Store:      data,
		AI:         aiClient,
		GitHub:     ghClient,
		Background: ctx,
	}

	payload := `{
		"action": "opened",
		"number": 42,
		"pull_request": {
			"number": 42,
			"title": "feat: test pr",
			"user": {"login": "octocat"},
			"draft": false
		},
		"repository": {
			"owner": {"login": "acme"},
			"name": "demo",
			"full_name": "acme/demo"
		}
	}`

	rowID := seedDelivery(t, data, "gh-del-pr-1", "pull_request", []byte(payload))
	svc.Process(rowID, "pull_request", "gh-del-pr-1", []byte(payload))

	// 等待后台异步审查与写库完成
	deadline := time.Now().Add(4 * time.Second)
	var reviewSetting store.SystemSetting
	var found bool
	for time.Now().Before(deadline) {
		item, err := data.WorkItems().GetByRepoNumber(ctx, "repo-demo", 42)
		if err == nil {
			setting, err := data.Settings().Get(ctx, "ai.pr_review."+item.ID)
			if err == nil && len(setting.ValueJSON) > 0 {
				reviewSetting = setting
				found = true
				break
			}
		}
		time.Sleep(50 * time.Millisecond)
	}

	if !found {
		t.Fatalf("expected ai.pr_review setting stored for PR 42, but timed out")
	}

	if !strings.Contains(string(reviewSetting.ValueJSON), "PR代码良好") || !strings.Contains(string(reviewSetting.ValueJSON), "92") {
		t.Fatalf("unexpected review content: %s", string(reviewSetting.ValueJSON))
	}
}

func TestServiceTriggerWorkItemReviewAndHighRiskAlert(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	ctx := t.Context()

	// 预先注册一个订阅 PR 的通道，以便接收高危警报
	_, err := data.Channels().Upsert(ctx, store.NotificationChannel{
		ID:          "ch-test-pr",
		ChannelType: "webhook",
		Name:        "test-hook",
		Target:      "https://example.com/webhook",
		Enabled:     true,
		EventKinds:  []string{store.WorkItemKindPR},
	})
	if err != nil {
		t.Fatalf("upsert channel failed: %v", err)
	}

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/42"):
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("diff --git a/vuln.go b/vuln.go\n+eval(userInput)\n"))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			res := map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": `{"summary":"存在高危漏洞","score":45,"security_risks":["代码注入危险"],"breaking_risks":["API破坏"],"code_smells":[]}`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer fakeServer.Close()

	aiClient := &ai.Client{
		Enabled:           true,
		BaseURL:           fakeServer.URL,
		Model:             "mock-model",
		APIKey:            "mock-key",
		CodeReviewEnabled: true,
	}
	ghClient := githubx.NewAppClient(1234, "")
	ghClient.BaseURL = fakeServer.URL

	svc := &webhooksvc.Service{
		Store:      data,
		AI:         aiClient,
		GitHub:     ghClient,
		Background: ctx,
	}

	// 先在库中放入 PR 工作项
	wi, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-test-42",
		RepositoryID:    "repo-demo",
		Kind:            store.WorkItemKindPR,
		Number:          42,
		Title:           "feat: dangerous eval",
		Author:          "hacker",
		State:           "open",
		SourceUpdatedAt: time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatalf("upsert work item failed: %v", err)
	}

	// 手动触发代码审查
	res, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("TriggerWorkItemReview failed: %v", err)
	}
	if res.Score != 45 || len(res.SecurityRisks) != 1 {
		t.Fatalf("unexpected review result: %+v", res)
	}

	// 验证优雅停机感知 WaitReviews 正常返回
	if err := svc.WaitReviews(ctx); err != nil {
		t.Fatalf("WaitReviews failed: %v", err)
	}

	// 验证 Outbox 是否产生了高危警报通知
	outboxItems, _, err := data.Outbox().List(ctx, store.ListFilter{})
	if err != nil {
		t.Fatalf("list outbox failed: %v", err)
	}
	if len(outboxItems) == 0 {
		t.Fatalf("expected outbox alert created for high risk PR review")
	}
	if !strings.Contains(outboxItems[0].Title, "代码审查告警") || !strings.Contains(outboxItems[0].BodyText, "代码注入危险") {
		t.Fatalf("unexpected outbox content: title=%s body=%s", outboxItems[0].Title, outboxItems[0].BodyText)
	}
}

func TestServiceTriggerWorkItemReviewEdgeCases(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	ctx := t.Context()

	aiClient := &ai.Client{
		Enabled:           true,
		APIKey:            "test-key",
		CodeReviewEnabled: true,
	}
	svc := &webhooksvc.Service{
		Store:      data,
		AI:         aiClient,
		Background: ctx,
	}

	// 1. 测试对非 PR（如 Issue）调用审查：应明确返回拒绝错误
	issue, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-issue-100",
		RepositoryID:    "repo-demo",
		Kind:            store.WorkItemKindIssue,
		Number:          100,
		Title:           "a bug report",
		Author:          "user",
		State:           "open",
		SourceUpdatedAt: time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatalf("upsert issue failed: %v", err)
	}

	_, err = svc.TriggerWorkItemReview(ctx, issue.ID)
	if err == nil || !strings.Contains(err.Error(), "not a pull request") {
		t.Fatalf("expected not a pull request error, got %v", err)
	}

	// 2. AI 审查未开启时的快速拒绝
	svcDisabled := &webhooksvc.Service{
		Store:      data,
		AI:         &ai.Client{Enabled: false},
		Background: ctx,
	}
	_, err = svcDisabled.TriggerWorkItemReview(ctx, issue.ID)
	if err == nil || !strings.Contains(err.Error(), "not enabled") {
		t.Fatalf("expected not enabled error, got %v", err)
	}
}

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

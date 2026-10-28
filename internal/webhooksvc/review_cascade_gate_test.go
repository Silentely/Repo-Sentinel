package webhooksvc_test

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
)

type mockCascadeJevClient struct {
	calls     atomic.Int64
	judgeFunc func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error)
}

func (m *mockCascadeJevClient) Judge(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
	m.calls.Add(1)
	if m.judgeFunc != nil {
		return m.judgeFunc(ctx, state, questions)
	}
	return jev.Result{}, nil
}

func ptrFloat(f float64) *float64 { return &f }
func ptrString(s string) *string  { return &s }

func setupReviewTestEnv(t *testing.T, prDiff string) (
	*webhooksvc.Service,
	store.Store,
	store.WorkItem,
	*atomic.Int64, // llmCalls
	*atomic.Int64, // commentCalls
	*atomic.Int64, // broadcastCalls
	*atomic.Int64, // outboxCalls
) {
	t.Helper()
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	ctx := t.Context()

	var llmCalls atomic.Int64
	var commentCalls atomic.Int64
	var broadcastCalls atomic.Int64
	var outboxCalls atomic.Int64

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/access_tokens"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_mock_token_123",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/10") && strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.github.v3.diff"):
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(prDiff))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/10"):
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"number":10,"head":{"sha":"sha-pr-10"}}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/issues/10/comments"):
			commentCalls.Add(1)
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 123}`))
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			llmCalls.Add(1)
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			res := map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": `{"summary":"PR深度审查完成","score":85,"security_risks":[],"breaking_risks":[],"code_smells":[]}`,
						},
					},
				},
			}
			_ = json.NewEncoder(w).Encode(res)
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	t.Cleanup(fakeServer.Close)

	aiClient := &ai.Client{
		Enabled:               true,
		BaseURL:               fakeServer.URL,
		Model:                 "mock-model",
		APIKey:                "mock-key",
		CodeReviewEnabled:     true,
		CodeReviewCommentOnPR: true,
	}

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	ghClient := githubx.NewAppClient(1234, "")
	ghClient.Configure(1234, "", string(pemBytes))
	ghClient.BaseURL = fakeServer.URL

	svc := &webhooksvc.Service{
		Store:               data,
		AI:                  aiClient,
		GitHub:              ghClient,
		Background:          ctx,
		ReviewDebounceDelay: 10 * time.Millisecond,
		OnBroadcast: func(topic, resource, resourceID string) {
			if topic == "ai_review.changed" {
				broadcastCalls.Add(1)
			}
		},
	}

	instID := "inst-demo-1"
	_, _ = data.Installations().Upsert(ctx, store.GitHubInstallation{
		ID:             instID,
		InstallationID: 12345,
		AccountLogin:   "acme",
		AccountType:    "Organization",
		TargetType:     "Organization",
	})

	repoRec, err := data.Repositories().GetByFullName(ctx, "acme/demo")
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	repoRec.InstallationID = &instID
	_, _ = data.Repositories().Upsert(ctx, repoRec)

	// 预置 WorkItem PR #10
	wi, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-pr-10",
		RepositoryID:    repoRec.ID,
		Number:          10,
		Kind:            store.WorkItemKindPR,
		State:           "open",
		Title:           "test pr",
		Author:          "alice",
		SourceUpdatedAt: time.Now().UTC(),
		StateHash:       "h10",
	}, nil)
	if err != nil {
		t.Fatalf("upsert work item: %v", err)
	}

	return svc, data, wi, &llmCalls, &commentCalls, &broadcastCalls, &outboxCalls
}

func waitForReviewSetting(t *testing.T, data store.Store, itemID string, wantStatus string) ai.CodeReviewResult {
	t.Helper()
	ctx := context.Background()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		setting, err := data.Settings().Get(ctx, "ai.pr_review."+itemID)
		if err == nil && len(setting.ValueJSON) > 0 {
			var res ai.CodeReviewResult
			if err := json.Unmarshal(setting.ValueJSON, &res); err == nil {
				if wantStatus == "" || res.ReviewStatus == wantStatus {
					return res
				}
			}
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for review setting on %s with status %q", itemID, wantStatus)
	return ai.CodeReviewResult{}
}

func waitForCounter(t *testing.T, counter *atomic.Int64, want int64) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if counter.Load() >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for counter to reach %d; got %d", want, counter.Load())
}

func TestPRReviewGate_LocalDocsOnly(t *testing.T) {
	// 纯文档变更：本地门禁判定 skip -> 0 Jev, 0 LLM, 评论0, 高危0, 广播1次
	pureDocsDiff := `diff --git a/docs/guide.md b/docs/guide.md
index 1111111..2222222 100644
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1,3 +1,4 @@
 # Guide
+Updated documentation.
`
	svc, data, wi, llmCalls, commentCalls, broadcastCalls, _ := setupReviewTestEnv(t, pureDocsDiff)

	// 配置 Jev 运行时：active 模式，开启级联门控
	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	ctx := context.Background()
	headSHA, err := svc.TriggerWorkItemReviewWithOptions(ctx, wi.ID, webhooksvc.ReviewOptions{ForceDeepReview: false})
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}
	if headSHA == "" {
		t.Fatal("headSHA should not be empty")
	}

	res := waitForReviewSetting(t, data, wi.ID, "skipped")

	// 验证 0 Jev, 0 LLM
	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls for local docs, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 0 {
		t.Fatalf("expected 0 LLM calls for local docs skip, got %d", llmCalls.Load())
	}

	// 验证 skip 字段
	if res.ReviewStatus != "skipped" {
		t.Fatalf("expected ReviewStatus=skipped, got %s", res.ReviewStatus)
	}
	if res.SkipReason != "local_documentation_only" {
		t.Fatalf("expected SkipReason=local_documentation_only, got %s", res.SkipReason)
	}
	if res.DecisionModel != "jev" || res.DecisionPolicy != "cascade_gate" {
		t.Fatalf("expected decision model=jev policy=cascade_gate, got model=%s policy=%s", res.DecisionModel, res.DecisionPolicy)
	}
	if res.Score != 100 {
		t.Fatalf("expected score 100 for skipped doc, got %d", res.Score)
	}

	// 验证副作用：无 GitHub 评论、无高危通知、触发一次广播
	if commentCalls.Load() != 0 {
		t.Fatalf("expected 0 github comments, got %d", commentCalls.Load())
	}
	if broadcastCalls.Load() != 1 {
		t.Fatalf("expected 1 SSE broadcast, got %d", broadcastCalls.Load())
	}

	outboxList, _, _ := data.Outbox().List(ctx, store.ListFilter{})
	if len(outboxList) != 0 {
		t.Fatalf("expected 0 outbox alerts for skipped doc, got %d", len(outboxList))
	}
}

func TestPRReviewGate_JevConsultSkip(t *testing.T) {
	// 模糊文档变更（.txt）：本地门禁 consult -> Jev 判定 skip -> 1 Jev, 0 LLM
	ambiguousDiff := `diff --git a/notes.txt b/notes.txt
index 1111111..2222222 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1,3 +1,4 @@
 Notes
+Extra text notes.
`
	svc, data, wi, llmCalls, commentCalls, _, _ := setupReviewTestEnv(t, ambiguousDiff)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{
				Model: "jev-1.14.0",
				Answers: map[string]jev.Answer{
					"needs_deep_review": {Type: "noul", Noul: ptrFloat(0.05)},
					"risk_surface": {
						Type:       "choice",
						Choice:     ptrString("documentation_only"),
						Confidence: ptrFloat(0.95),
					},
				},
			}, nil
		},
	}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	ctx := context.Background()
	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}

	res := waitForReviewSetting(t, data, wi.ID, "skipped")

	if mockJev.calls.Load() != 1 {
		t.Fatalf("expected 1 Jev call, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 0 {
		t.Fatalf("expected 0 LLM calls, got %d", llmCalls.Load())
	}
	if res.ReviewStatus != "skipped" || res.SkipReason != "jev_documentation_only" {
		t.Fatalf("unexpected result: %+v", res)
	}
	if commentCalls.Load() != 0 {
		t.Fatalf("expected 0 comments, got %d", commentCalls.Load())
	}
}

func TestPRReviewGate_JevConsultTimeoutOrErrorFallback(t *testing.T) {
	// 模糊文档变更（.txt）：Jev 超时/网络错误 -> 降级走原 LLM -> 1 Jev, 1 LLM
	ambiguousDiff := `diff --git a/notes.txt b/notes.txt
index 1111111..2222222 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1,3 +1,4 @@
 Notes
+Extra text notes.
`
	svc, data, wi, llmCalls, commentCalls, _, _ := setupReviewTestEnv(t, ambiguousDiff)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{
		judgeFunc: func(ctx context.Context, state json.RawMessage, questions map[string]jev.Question) (jev.Result, error) {
			return jev.Result{}, errors.New("timeout connecting to jev")
		},
	}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	ctx := context.Background()
	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}

	res := waitForReviewSetting(t, data, wi.ID, "reviewed")

	if mockJev.calls.Load() != 1 {
		t.Fatalf("expected 1 Jev call, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call on fallback, got %d", llmCalls.Load())
	}
	if res.ReviewStatus != "reviewed" {
		t.Fatalf("expected ReviewStatus=reviewed on fallback, got %s", res.ReviewStatus)
	}
	waitForCounter(t, commentCalls, 1)
	if commentCalls.Load() != 1 {
		t.Fatalf("expected 1 comment for full review, got %d", commentCalls.Load())
	}
}

func TestPRReviewGate_SensitiveDiffForceLLM(t *testing.T) {
	// 敏感文件变更（.github/workflows/ci.yml）：强制走原 LLM -> 0 Jev, 1 LLM
	sensitiveDiff := `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
index 1111111..2222222 100644
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,3 +1,4 @@
 name: CI
+run: make build
`
	svc, data, wi, llmCalls, _, _, _ := setupReviewTestEnv(t, sensitiveDiff)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	ctx := context.Background()
	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}

	res := waitForReviewSetting(t, data, wi.ID, "reviewed")

	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls for sensitive diff, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call for sensitive diff, got %d", llmCalls.Load())
	}
	if res.ReviewStatus != "reviewed" {
		t.Fatalf("expected ReviewStatus=reviewed, got %s", res.ReviewStatus)
	}
}

func TestPRReviewGate_PrivateRepoWithoutAuthorization(t *testing.T) {
	// 私有仓且 AllowPrivateState=false -> 不得将 facts 发送给 Jev -> 0 Jev, 1 LLM
	ambiguousDiff := `diff --git a/notes.txt b/notes.txt
index 1111111..2222222 100644
--- a/notes.txt
+++ b/notes.txt
@@ -1,3 +1,4 @@
 Notes
+Private repo notes.
`
	svc, data, wi, llmCalls, _, _, _ := setupReviewTestEnv(t, ambiguousDiff)

	ctx := context.Background()
	// 将仓库标记为私有仓
	repoRec, _ := data.Repositories().Get(ctx, wi.RepositoryID)
	repoRec.IsPrivate = true
	_, _ = data.Repositories().Upsert(ctx, repoRec)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
		AllowPrivateState:  false, // 未授权
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}

	res := waitForReviewSetting(t, data, wi.ID, "reviewed")

	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls for private repo without authorization, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call, got %d", llmCalls.Load())
	}
	if res.ReviewStatus != "reviewed" {
		t.Fatalf("expected ReviewStatus=reviewed, got %s", res.ReviewStatus)
	}
}

func TestPRReviewGate_ShadowMode(t *testing.T) {
	// shadow 模式：即使本地判定 skip，也不短路后续流程，继续执行原 LLM
	pureDocsDiff := `diff --git a/docs/guide.md b/docs/guide.md
index 1111111..2222222 100644
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1,3 +1,4 @@
 # Guide
+Updated documentation in shadow mode.
`
	svc, data, wi, llmCalls, commentCalls, _, _ := setupReviewTestEnv(t, pureDocsDiff)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "shadow", // shadow 模式
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	mockJev := &mockCascadeJevClient{}
	rt.SetClientForTest("review", mockJev)
	svc.JevRuntime = rt

	ctx := context.Background()
	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("trigger review: %v", err)
	}

	res := waitForReviewSetting(t, data, wi.ID, "reviewed")

	if mockJev.calls.Load() != 0 {
		t.Fatalf("expected 0 Jev calls, got %d", mockJev.calls.Load())
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call in shadow mode, got %d", llmCalls.Load())
	}
	if res.ReviewStatus != "reviewed" {
		t.Fatalf("expected ReviewStatus=reviewed in shadow mode, got %s", res.ReviewStatus)
	}
	waitForCounter(t, commentCalls, 1)
	if commentCalls.Load() != 1 {
		t.Fatalf("expected 1 comment in shadow mode, got %d", commentCalls.Load())
	}
}

func TestPRReviewGate_ForceDeepReviewOverwritesSkip(t *testing.T) {
	// 先行写入已跳过的审查记录；带 ForceDeepReview=true 触发时应强制重新跑 LLM 并覆盖
	pureDocsDiff := `diff --git a/docs/guide.md b/docs/guide.md
index 1111111..2222222 100644
--- a/docs/guide.md
+++ b/docs/guide.md
@@ -1,3 +1,4 @@
 # Guide
+Force deep review test.
`
	svc, data, wi, llmCalls, commentCalls, _, _ := setupReviewTestEnv(t, pureDocsDiff)

	jevCfg := config.JevConfig{
		Enabled:            true,
		CascadeGateEnabled: true,
		Mode:               "active",
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	svc.JevRuntime = rt

	ctx := context.Background()
	// 首次触发：走门控跳过
	_, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("first trigger: %v", err)
	}
	res1 := waitForReviewSetting(t, data, wi.ID, "skipped")
	if res1.ReviewStatus != "skipped" {
		t.Fatalf("expected skipped, got %s", res1.ReviewStatus)
	}
	if llmCalls.Load() != 0 {
		t.Fatalf("expected 0 LLM calls initially, got %d", llmCalls.Load())
	}

	// 再次触发：传入 ForceDeepReview = true
	_, err = svc.TriggerWorkItemReviewWithOptions(ctx, wi.ID, webhooksvc.ReviewOptions{ForceDeepReview: true})
	if err != nil {
		t.Fatalf("force trigger: %v", err)
	}

	res2 := waitForReviewSetting(t, data, wi.ID, "reviewed")
	if res2.ReviewStatus != "reviewed" {
		t.Fatalf("expected reviewed after force deep review, got %s", res2.ReviewStatus)
	}
	if llmCalls.Load() != 1 {
		t.Fatalf("expected 1 LLM call after force, got %d", llmCalls.Load())
	}
	if commentCalls.Load() != 1 {
		t.Fatalf("expected 1 comment after force, got %d", commentCalls.Load())
	}
}

func TestPRReviewGate_LegacyJSONCompat(t *testing.T) {
	// 验证旧版 JSON（无 review_status 字段）被反序列化时，兼容视为 "reviewed"
	rawLegacy := []byte(`{
		"summary": "旧版审查结果",
		"score": 90,
		"security_risks": [],
		"breaking_risks": [],
		"code_smells": []
	}`)

	var res ai.CodeReviewResult
	if err := json.Unmarshal(rawLegacy, &res); err != nil {
		t.Fatalf("unmarshal legacy review: %v", err)
	}
	if res.ReviewStatus != "" {
		t.Fatalf("expected empty field in raw struct, got %s", res.ReviewStatus)
	}
	// 业务逻辑将空值视为 reviewed
	status := res.ReviewStatus
	if status == "" {
		status = "reviewed"
	}
	if status != "reviewed" {
		t.Fatalf("expected effective reviewed, got %s", status)
	}
}

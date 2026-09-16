package webhooksvc_test

import (
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
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
	"github.com/oklog/ulid/v2"
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

	// 构造 AI 客户端并开启 PR 审查与回写
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
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/42") && strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.github.v3.diff"):
			// Diff 接口（GetPRDiff 以 diff 专有 Accept 头区分详情接口）
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("diff --git a/vuln.go b/vuln.go\n+eval(userInput)\n"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/42"):
			// 详情接口（GetPRDetail 获取 head SHA）
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"head":{"sha":"feedface1234"}}`))
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

	// 手动触发代码审查：异步入队并返回 head SHA 供轮询比对
	headSHA, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("TriggerWorkItemReview failed: %v", err)
	}
	if headSHA != "feedface1234" {
		t.Fatalf("unexpected head sha: %q", headSHA)
	}

	// 轮询等待后台审查落库，且报告的 head SHA 与回执一致
	deadline := time.Now().Add(4 * time.Second)
	var reviewSetting store.SystemSetting
	var found bool
	for time.Now().Before(deadline) {
		setting, err := data.Settings().Get(ctx, "ai.pr_review."+wi.ID)
		if err == nil && strings.Contains(string(setting.ValueJSON), headSHA) {
			reviewSetting = setting
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected ai.pr_review setting stored for work item, but timed out")
	}
	if !strings.Contains(string(reviewSetting.ValueJSON), "35") || !strings.Contains(string(reviewSetting.ValueJSON), "代码注入危险") {
		t.Fatalf("unexpected review content: %s", string(reviewSetting.ValueJSON))
	}

	// 验证优雅停机感知 WaitReviews 正常返回
	if err := svc.WaitReviews(ctx); err != nil {
		t.Fatalf("WaitReviews failed: %v", err)
	}

	// 验证 Outbox 是否产生了高危警报通知：告警入队在报告持久化之后异步完成，
	// 且测试直构 Service 未装配 reviewTracker（WaitReviews 为空操作），需轮询等待
	var outboxItems []store.NotificationOutbox
	deadline = time.Now().Add(4 * time.Second)
	for {
		items, _, err := data.Outbox().List(ctx, store.ListFilter{})
		if err != nil {
			t.Fatalf("list outbox failed: %v", err)
		}
		if len(items) > 0 {
			outboxItems = items
			break
		}
		if !time.Now().Before(deadline) {
			t.Fatalf("expected outbox alert created for high risk PR review")
		}
		time.Sleep(50 * time.Millisecond)
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

	// 先在库中放入 PR 工作项（能力/依赖开关的校验在其后，需要真实存在的 PR 目标）。
	pr, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-pr-101",
		RepositoryID:    "repo-demo",
		Kind:            store.WorkItemKindPR,
		Number:          101,
		Title:           "clean change",
		Author:          "dev",
		State:           "open",
		SourceUpdatedAt: time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatalf("upsert pr failed: %v", err)
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
	if !errors.Is(err, webhooksvc.ErrInvalidReviewTarget) {
		t.Fatalf("expected ErrInvalidReviewTarget, got %v", err)
	}

	// 2. AI 审查未开启时的快速拒绝
	svcDisabled := &webhooksvc.Service{
		Store:      data,
		AI:         &ai.Client{Enabled: false},
		Background: ctx,
	}
	_, err = svcDisabled.TriggerWorkItemReview(ctx, pr.ID)
	if !errors.Is(err, webhooksvc.ErrReviewNotEnabled) {
		t.Fatalf("expected ErrReviewNotEnabled, got %v", err)
	}

	// 3. GitHub 客户端缺失（无法拉取详情/Diff）：依赖不可用
	svcNoGitHub := &webhooksvc.Service{
		Store:      data,
		AI:         aiClient,
		Background: ctx,
	}
	_, err = svcNoGitHub.TriggerWorkItemReview(ctx, pr.ID)
	if !errors.Is(err, webhooksvc.ErrReviewUnavailable) {
		t.Fatalf("expected ErrReviewUnavailable, got %v", err)
	}
}

func TestIsBotUser(t *testing.T) {
	cases := []struct {
		login    string
		userType string
		want     bool
	}{
		{"dependabot[bot]", "User", true},
		{"renovate[bot]", "Bot", true},
		{"github-actions[bot]", "", true},
		{"some-service-app", "Bot", true},
		{"dependabot", "", true},
		{"renovate", "", true},
		{"github-actions", "", true},
		{"snyk-bot", "", true},
		{"codecov", "", true},
		{"alice", "User", false},
		{"bob", "", false},
		{"", "", false},
	}

	for _, tc := range cases {
		got := webhooksvc.IsBotUser(tc.login, tc.userType)
		if got != tc.want {
			t.Errorf("IsBotUser(%q, %q) = %v, want %v", tc.login, tc.userType, got, tc.want)
		}
	}
}

func TestServiceAICodeReviewBotPR(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	ctx := t.Context()

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/43") && strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.github.v3.diff"):
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("diff --git a/go.mod b/go.mod\n+require example.com/pkg v1.2.3\n"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/43"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"head": map[string]any{"sha": "botsha123"},
			})
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "application/json")
			res := map[string]any{
				"choices": []map[string]any{
					{
						"message": map[string]any{
							"content": `{"summary":"机器人依赖升级","score":90,"security_risks":[],"breaking_risks":[],"code_smells":[]}`,
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

	// 1. 模拟 dependabot 提交 PR Webhook：应默认跳过自动审查
	payload := `{
		"action": "opened",
		"number": 43,
		"pull_request": {
			"number": 43,
			"title": "chore(deps): bump pkg from 1.2.2 to 1.2.3",
			"user": {"login": "dependabot[bot]", "type": "Bot"},
			"draft": false,
			"head": {"sha": "botsha123"}
		},
		"repository": {
			"owner": {"login": "acme"},
			"name": "demo",
			"full_name": "acme/demo"
		}
	}`

	rowID := seedDelivery(t, data, "gh-del-bot-pr", "pull_request", []byte(payload))
	svc.Process(rowID, "pull_request", "gh-del-bot-pr", []byte(payload))

	// 验证工作项被正常写入，但未自动生成审查设置
	item, err := data.WorkItems().GetByRepoNumber(ctx, "repo-demo", 43)
	if err != nil {
		t.Fatalf("expected work item created for bot PR: %v", err)
	}

	// 稍微等待确认后台没有发起自动审查
	time.Sleep(200 * time.Millisecond)
	setting, err := data.Settings().Get(ctx, "ai.pr_review."+item.ID)
	if err == nil && len(setting.ValueJSON) > 0 {
		t.Fatalf("bot PR should not be reviewed automatically, but got: %s", string(setting.ValueJSON))
	}

	// 2. 用户手动触发审查该机器人 PR
	headSHA, err := svc.TriggerWorkItemReview(ctx, item.ID)
	if err != nil {
		t.Fatalf("manual trigger review for bot PR failed: %v", err)
	}
	if headSHA != "botsha123" {
		t.Fatalf("expected headSHA botsha123, got %s", headSHA)
	}

	// 等待手动审查完成落库
	deadline := time.Now().Add(4 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		setting, err := data.Settings().Get(ctx, "ai.pr_review."+item.ID)
		if err == nil && strings.Contains(string(setting.ValueJSON), "botsha123") {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected manual review result stored for bot PR, but timed out")
	}
}

func TestServiceTriggerWorkItemReviewPrivateRepo(t *testing.T) {
	data := openServiceStore(t)
	ctx := t.Context()

	// 1. 生成并配置 RSA 密钥给 GitHub AppClient
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	instID := ulid.Make().String()
	const ghInstallationID = int64(98765)

	// 2. 创建私密仓库：InstallationID 存储为 installations 表的主键 ULID（复现线上数据结构）
	privRepo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-priv",
		Type:           store.RepositoryTypeInstallation,
		SyncStatus:     store.SyncStatusActive,
		Owner:          "acme",
		Name:           "secret",
		FullName:       "acme/secret",
		IsPrivate:      true,
		InstallationID: &instID,
		HTMLURL:        "https://github.com/acme/secret",
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	wi, _, err := data.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-priv-1",
		RepositoryID:    privRepo.ID,
		Kind:            store.WorkItemKindPR,
		Number:          1,
		Title:           "feat: secret patch",
		Author:          "alice",
		State:           "open",
		SourceUpdatedAt: time.Now().UTC(),
	}, nil)
	if err != nil {
		t.Fatalf("upsert wi: %v", err)
	}

	var prDetailAuthorized atomic.Bool

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/access_tokens"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token":      "ghs_secret_mock_token_123",
				"expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339),
			})
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/1") && strings.HasPrefix(r.Header.Get("Accept"), "application/vnd.github.v3.diff"):
			w.Header().Set("Content-Type", "application/vnd.github.v3.diff")
			_, _ = w.Write([]byte("diff --git a/a.txt b/a.txt\n+hello secret\n"))
		case r.Method == http.MethodGet && strings.HasSuffix(r.URL.Path, "/pulls/1"):
			auth := r.Header.Get("Authorization")
			if auth == "Bearer ghs_secret_mock_token_123" {
				prDetailAuthorized.Store(true)
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(`{"head":{"sha":"privsha999"}}`))
				return
			}
			// 未带正确令牌时 GitHub 私密仓返回 404（复现用户报出的 404 故障）
			http.NotFound(w, r)
		case r.Method == http.MethodPost && strings.HasSuffix(r.URL.Path, "/chat/completions"):
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"choices": []map[string]any{
					{"message": map[string]any{"content": `{"summary":"私有仓库PR审查良好","score":95,"security_risks":[],"breaking_risks":[],"code_smells":[]}`}},
				},
			})
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))
	defer fakeServer.Close()

	ghClient := githubx.NewAppClient(1234, "")
	ghClient.Configure(1234, "", string(pemBytes))
	ghClient.BaseURL = fakeServer.URL

	aiClient := &ai.Client{
		Enabled:           true,
		BaseURL:           fakeServer.URL,
		Model:             "mock-model",
		APIKey:            "mock-key",
		CodeReviewEnabled: true,
	}

	svc := &webhooksvc.Service{
		Store:      data,
		AI:         aiClient,
		GitHub:     ghClient,
		Background: ctx,
	}

	// 阶段 1：本地未存入该 ULID 的 installation 记录时，私密仓无可用 token，应快速拒绝并返回 ErrReviewUnavailable
	_, err = svc.TriggerWorkItemReview(ctx, wi.ID)
	if !errors.Is(err, webhooksvc.ErrReviewUnavailable) {
		t.Fatalf("expected ErrReviewUnavailable when installation token missing for private repo, got %v", err)
	}

	// 阶段 2：插入与 repo.InstallationID (ULID) 匹配的 GitHubInstallation 记录
	_, err = data.Installations().Upsert(ctx, store.GitHubInstallation{
		ID:             instID,
		InstallationID: ghInstallationID,
		AccountLogin:   "acme",
		AccountType:    "Organization",
		TargetType:     "Organization",
	})
	if err != nil {
		t.Fatalf("upsert installation: %v", err)
	}

	// 阶段 3：再次手动触发，应成功通过 ULID 关联查到安装 ID 并获取 token，正常拉取私有 PR 详情并入队
	headSHA, err := svc.TriggerWorkItemReview(ctx, wi.ID)
	if err != nil {
		t.Fatalf("TriggerWorkItemReview failed for private repo: %v", err)
	}
	if headSHA != "privsha999" {
		t.Fatalf("expected head sha privsha999, got %q", headSHA)
	}
	if !prDetailAuthorized.Load() {
		t.Fatalf("expected GetPRDetail to be called with Authorization token")
	}

	// 等待后台审查落库完成
	deadline := time.Now().Add(4 * time.Second)
	var found bool
	for time.Now().Before(deadline) {
		setting, err := data.Settings().Get(ctx, "ai.pr_review."+wi.ID)
		if err == nil && strings.Contains(string(setting.ValueJSON), "privsha999") {
			found = true
			break
		}
		time.Sleep(50 * time.Millisecond)
	}
	if !found {
		t.Fatalf("expected review setting persisted for private repo PR")
	}
}

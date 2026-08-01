package syncx

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"net/http"
	"net/http/httptest"
	"regexp"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// 伪 GitHub API 路由：Reconciler 经由 AppClient 发出的全部请求都由 httptest 拦截，杜绝真实网络。
var (
	reAccessTokens = regexp.MustCompile(`^/app/installations/\d+/access_tokens$`)
	reIssues       = regexp.MustCompile(`^/repos/[^/]+/[^/]+/issues$`)
	reActionsRuns  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/actions/runs$`)
	reDependabot   = regexp.MustCompile(`^/repos/[^/]+/[^/]+/dependabot/alerts$`)
	reCodeScanning = regexp.MustCompile(`^/repos/[^/]+/[^/]+/code-scanning/alerts$`)
	reSecretScan   = regexp.MustCompile(`^/repos/[^/]+/[^/]+/secret-scanning/alerts$`)
	rePRReviews    = regexp.MustCompile(`^/repos/[^/]+/[^/]+/pulls/(\d+)/reviews$`)
	rePRReviewers  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/pulls/(\d+)/requested_reviewers$`)
	rePRDetail     = regexp.MustCompile(`^/repos/[^/]+/[^/]+/pulls/(\d+)$`)
	rePRCheckRuns  = regexp.MustCompile(`^/repos/[^/]+/[^/]+/commits/[^/]+/check-runs$`)
)

// fakeGitHub 伪 GitHub API 服务：按路由返回可配置响应，并用原子计数器观测请求次数。
type fakeGitHub struct {
	server *httptest.Server
	client *githubx.AppClient

	// issuesFn 按页返回 issues 响应体；用例可定制，默认空页。
	issuesFn func(page int) any
	// dependabotFn 按 after 游标返回 dependabot alerts 响应体与下一页游标；
	// 返回的 next 非空时写入 Link header 模拟游标分页；用例可定制，默认空页。
	dependabotFn func(cursor string) (any, string)
	// alertHTTPStatus 可选：code/secret scanning 端点的状态码（默认 200），
	// 用于模拟功能未开启（404）等不可用场景。
	alertHTTPStatus map[string]int
	// rateLimitIssuesRequests 前 N 次 issues 请求返回 429 + Retry-After 响应头；
	// issues429RetryAfter 缺省 "60"。N=0 表示不限流。
	rateLimitIssuesRequests int
	issues429RetryAfter     string

	// 请求计数器（httptest 每个请求独立 goroutine，必须用原子量）。
	prDetailRequests atomic.Int64 // GET /pulls/{n}：enrich 预算观测点
	dependabotPages  atomic.Int64 // dependabot alerts 请求总次数
	issuesPages      atomic.Int64 // issues 请求总次数（能力开关观测点）
	actionsPages     atomic.Int64 // actions runs 请求总次数（能力开关观测点）
}

// ServeHTTP 实现 http.Handler，把 Reconciler 可能触达的端点全部接管。
func (f *fakeGitHub) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	write := func(v any) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(v)
	}
	page, _ := strconv.Atoi(r.URL.Query().Get("page"))
	cursor := r.URL.Query().Get("after")
	path := r.URL.Path
	switch {
	case reAccessTokens.MatchString(path):
		// Installation Token 签发：任意 JWT 都接受，直接返回测试 token。
		write(map[string]any{"token": "test-installation-token", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	case reIssues.MatchString(path):
		f.issuesPages.Add(1)
		if f.rateLimitIssuesRequests > 0 {
			f.rateLimitIssuesRequests--
			ra := f.issues429RetryAfter
			if ra == "" {
				ra = "60"
			}
			w.Header().Set("Retry-After", ra)
			http.Error(w, "rate limited", http.StatusTooManyRequests)
			return
		}
		if f.issuesFn != nil {
			write(f.issuesFn(page))
			return
		}
		write([]any{})
	case reActionsRuns.MatchString(path):
		f.actionsPages.Add(1)
		write(map[string]any{"workflow_runs": []any{}})
	case reDependabot.MatchString(path):
		f.dependabotPages.Add(1)
		if f.dependabotFn != nil {
			items, next := f.dependabotFn(cursor)
			if next != "" {
				w.Header().Set("Link", fmt.Sprintf(`<%s/repos/acme/demo/dependabot/alerts?after=%s&per_page=50>; rel="next"`, f.server.URL, next))
			}
			write(items)
			return
		}
		write([]any{})
	case reCodeScanning.MatchString(path):
		if status, ok := f.alertHTTPStatus["code_scanning"]; ok {
			http.Error(w, http.StatusText(status), status)
			return
		}
		write([]any{})
	case reSecretScan.MatchString(path):
		if status, ok := f.alertHTTPStatus["secret_scanning"]; ok {
			http.Error(w, http.StatusText(status), status)
			return
		}
		write([]any{})
	case rePRReviews.MatchString(path):
		write([]map[string]any{{
			"id": 1, "state": "APPROVED", "user": map[string]any{"login": "reviewer"},
			"submitted_at": time.Now().UTC().Format(time.RFC3339),
		}})
	case rePRReviewers.MatchString(path):
		write(map[string]any{"users": []map[string]any{{"login": "octocat"}}, "teams": []any{}})
	case rePRDetail.MatchString(path):
		f.prDetailRequests.Add(1)
		num := rePRDetail.FindStringSubmatch(path)[1]
		write(map[string]any{"head": map[string]any{"sha": "head-sha-" + num}})
	case rePRCheckRuns.MatchString(path):
		write(map[string]any{"check_runs": []map[string]any{
			{"id": 1, "name": "ci", "status": "completed", "conclusion": "success"},
			{"id": 2, "name": "lint", "status": "completed", "conclusion": "success"},
		}})
	default:
		// 未知路径直接 500：让对账流程尽快暴露测试覆盖缺口。
		http.Error(w, "unexpected github api path: "+path, http.StatusInternalServerError)
	}
}

// newReconcileFixture 构建对账测试环境：
// 真实 sqlite store（t.TempDir）+ 伪 GitHub API + 内存 RSA 私钥配置的 AppClient + installation 类型仓。
func newReconcileFixture(t *testing.T) (store.Store, *fakeGitHub, store.Repository) {
	t.Helper()
	data := openSyncStore(t)
	ctx := t.Context()

	// 内存 RSA 私钥（与 githubx 测试同规格），满足 AppClient.Configured 的前置校验。
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	fake := &fakeGitHub{}
	fake.server = httptest.NewServer(fake)
	t.Cleanup(fake.server.Close)
	// 与生产构造路径一致（bootstrap 用 NewAppClient、设置热更新用 Configure）：
	// token 缓存 map 只在这两条路径初始化，裸结构体字面量会在 InstallationToken 赋值时 panic。
	fake.client = githubx.NewAppClient(0, "")
	fake.client.HTTP = fake.server.Client()
	fake.client.BaseURL = fake.server.URL
	fake.client.Configure(12345, "", string(pemBytes))

	inst, err := data.Installations().Upsert(ctx, store.GitHubInstallation{
		ID: ulid.Make().String(), InstallationID: 4242,
		AccountLogin: "acme", AccountType: "Organization", TargetType: "Organization",
	})
	if err != nil {
		t.Fatal(err)
	}
	instID := inst.ID
	repo, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo", InstallationID: &instID,
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	return data, fake, repo
}

// TestReconcileRepositoryInstallationSuccess 覆盖 installation 仓的完整成功路径：
// 伪 API 返回 1 个纯 issue + 1 个带 PR 链接的条目，断言落库字段、PR enrich 结果与仓库同步时间。
func TestReconcileRepositoryInstallationSuccess(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	updated := time.Now().UTC().Truncate(time.Second)

	fake.issuesFn = func(page int) any {
		return []map[string]any{
			{
				"number": 1, "state": "open", "title": "修复登录崩溃",
				"html_url":   "https://github.com/acme/demo/issues/1",
				"updated_at": updated.Format(time.RFC3339),
				"user":       map[string]any{"login": "alice"},
				"labels":     []any{}, "assignees": []any{},
			},
			{
				"number": 2, "state": "open", "title": "增加缓存层",
				"html_url":     "https://github.com/acme/demo/pull/2",
				"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/demo/pulls/2"},
				"updated_at":   updated.Format(time.RFC3339),
				"user":         map[string]any{"login": "bob"},
				"labels":       []map[string]any{{"name": "enhancement"}}, "assignees": []any{},
			},
		}
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("成功路径不应报错: %v", err)
	}

	// 断言 WorkItems 落库：kind/state/html_url 必须忠实于 API 响应。
	items, page1, err := data.WorkItems().List(ctx, store.ListFilter{Page: 1, PerPage: 20, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if page1.Total != 2 || len(items) != 2 {
		t.Fatalf("应落库 2 条 WorkItem，total=%d len=%d", page1.Total, len(items))
	}

	issue, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 1)
	if err != nil {
		t.Fatal(err)
	}
	if issue.Kind != store.WorkItemKindIssue {
		t.Fatalf("#1 应为 issue，got %s", issue.Kind)
	}
	if issue.State != "open" {
		t.Fatalf("#1 state 应为 open，got %s", issue.State)
	}
	if issue.HTMLURL != "https://github.com/acme/demo/issues/1" {
		t.Fatalf("#1 html_url 不符，got %s", issue.HTMLURL)
	}

	pr, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 2)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Kind != store.WorkItemKindPR {
		t.Fatalf("#2 带 pull_request 字段应识别为 PR，got %s", pr.Kind)
	}
	if pr.HTMLURL != "https://github.com/acme/demo/pull/2" {
		t.Fatalf("#2 html_url 不符，got %s", pr.HTMLURL)
	}
	// active 状态下 PR 必须走 enrich 并回填审核/检查结论。
	if pr.ReviewState != "APPROVED" || pr.ReviewDecision != "approved" {
		t.Fatalf("#2 审核结论 enrich 失败，state=%s decision=%s", pr.ReviewState, pr.ReviewDecision)
	}
	if pr.CheckStatus != "success" || pr.ChecksTotal != 2 || pr.ChecksPassed != 2 {
		t.Fatalf("#2 检查状态 enrich 失败，status=%s total=%d passed=%d", pr.CheckStatus, pr.ChecksTotal, pr.ChecksPassed)
	}
	if len(pr.Reviewers) != 1 || pr.Reviewers[0] != "octocat" {
		t.Fatalf("#2 评审人 enrich 失败，got %v", pr.Reviewers)
	}

	// 仓库 last_synced_at 必须被刷新，且全链路成功时不留错误码。
	got, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncedAt == nil {
		t.Fatal("last_synced_at 应被更新")
	}
	if got.LastSyncErrorCode != "" {
		t.Fatalf("全成功不应有错误码，got %s", got.LastSyncErrorCode)
	}
	// issues 游标应写盘，供下轮增量同步。
	if _, err := data.Cursors().Get(ctx, repo.ID, "issues"); err != nil {
		t.Fatalf("issues 游标应落库: %v", err)
	}
}

// TestReconcileDependabotAlertsPagination 验证 dependabot 告警游标分页：
// 第一页满 50 条且 Link header 给出下一页游标时必须继续拉取，51 条告警须全部落库
// （防回归"只拉第一页"，且 dependabot 端点不接受 page 参数，必须走 after 游标）。
func TestReconcileDependabotAlertsPagination(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	updated := time.Now().UTC().Truncate(time.Second)

	alert := func(n int) map[string]any {
		return map[string]any{
			"number": n, "state": "open",
			"html_url":   fmt.Sprintf("https://github.com/acme/demo/security/dependabot/%d", n),
			"created_at": updated.Format(time.RFC3339), "updated_at": updated.Format(time.RFC3339),
			"dependency":        map[string]any{"package": map[string]any{"name": "lodash"}},
			"security_advisory": map[string]any{"severity": "high"},
		}
	}
	fake.issuesFn = func(page int) any { return []any{} }
	fake.dependabotFn = func(cursor string) (any, string) {
		switch cursor {
		case "":
			items := make([]map[string]any, 0, 50)
			for i := 1; i <= 50; i++ {
				items = append(items, alert(i))
			}
			return items, "cursor-2"
		case "cursor-2":
			return []map[string]any{alert(51)}, ""
		default:
			return []any{}, ""
		}
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	if got := fake.dependabotPages.Load(); got < 2 {
		t.Fatalf("第一页满 50 条时应按游标继续拉第二页，实际请求 %d 次", got)
	}
	_, pageResult, err := data.SecurityAlerts().List(ctx, store.ListFilter{Page: 1, PerPage: 100, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if pageResult.Total != 51 {
		t.Fatalf("两页共 51 条告警应全部落库，total=%d", pageResult.Total)
	}
	// 抽查第二页唯一一条的字段映射（依赖名与 advisory 严重度回填）。
	last, err := data.SecurityAlerts().GetByIdentity(ctx, repo.ID, store.AlertKindDependabot, 51)
	if err != nil {
		t.Fatal(err)
	}
	if last.RuleOrDependency != "lodash" || last.Severity != "high" {
		t.Fatalf("告警字段映射错误: rule=%s severity=%s", last.RuleOrDependency, last.Severity)
	}
}

// TestReconcileAlertsUnavailableNotPartial 验证仓库未开启 code/secret scanning
// （GitHub 返回 404）时对账静默跳过该类告警，不标记 reconcile_partial：
// 功能未开启是静态状态，反复重试结果不变，不应污染"最后同步"状态。
func TestReconcileAlertsUnavailableNotPartial(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	fake.issuesFn = func(page int) any { return []any{} }
	fake.alertHTTPStatus = map[string]int{
		"code_scanning":   http.StatusNotFound,
		"secret_scanning": http.StatusNotFound,
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	got, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncErrorCode != "" {
		t.Fatalf("功能未开启不应标记部分失败，got %s", got.LastSyncErrorCode)
	}
}

// TestReconcilePREnrichBudget 验证单轮 PR enrich 预算上限：
// 30 个待 enrich 的新 PR 只允许 25 次 PR 详情请求，超预算的 PR 仍须正常落库。
// TestReconcileRespectsCapabilityToggles 验证能力开关对对账的分流：
// 关闭 PR/Actions/告警后，仅 Issues 被采集，对应 GitHub API 完全不被请求。
func TestReconcileRespectsCapabilityToggles(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	updated := time.Now().UTC().Truncate(time.Second)

	fake.issuesFn = func(page int) any {
		return []map[string]any{
			{
				"number": 1, "state": "open", "title": "保留的 issue",
				"html_url":   "https://github.com/acme/demo/issues/1",
				"updated_at": updated.Format(time.RFC3339),
				"user":       map[string]any{"login": "alice"},
				"labels":     []any{}, "assignees": []any{},
			},
			{
				"number": 2, "state": "open", "title": "被开关关闭的 PR",
				"html_url":     "https://github.com/acme/demo/pull/2",
				"pull_request": map[string]any{"url": "https://api.github.com/repos/acme/demo/pulls/2"},
				"updated_at":   updated.Format(time.RFC3339),
				"user":         map[string]any{"login": "bob"},
				"labels":       []any{}, "assignees": []any{},
			},
		}
	}

	off := false
	if err := data.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{
		PrEnabled: &off, ActionsEnabled: &off, AlertsEnabled: &off,
	}); err != nil {
		t.Fatal(err)
	}
	// ReconcileRepository 消费的是传入快照：必须取回开关更新后的仓库。
	repo, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	if got := fake.actionsPages.Load(); got != 0 {
		t.Fatalf("Actions 关闭不应请求 actions runs，got %d", got)
	}
	if got := fake.dependabotPages.Load(); got != 0 {
		t.Fatalf("告警关闭不应请求 dependabot，got %d", got)
	}
	if _, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 1); err != nil {
		t.Fatalf("issue 应正常采集: %v", err)
	}
	if _, err := data.WorkItems().GetByRepoNumber(ctx, repo.ID, 2); err == nil {
		t.Fatal("PR 开关关闭后不应采集 PR")
	}
}

// 全局 feature.actions 关闭：即使仓库级 Actions 开启也不请求 workflow runs。
func TestReconcileRespectsGlobalFeatureActions(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	raw, _ := json.Marshal(false)
	if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
		ID: "feat-actions-off", Key: store.SettingFeatureActions, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if !repo.ActionsEnabled {
		t.Fatal("仓库级 Actions 应默认开启")
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	if got := fake.actionsPages.Load(); got != 0 {
		t.Fatalf("全局 Actions 关闭不应请求 actions runs，got %d", got)
	}
}

// TestAlertUnavailableClassification 表驱动守护告警不可用判定：
// 仅 HTTP 状态码按 kind 命中才静默跳过，其余错误视为临时故障参与重试。
func TestAlertUnavailableClassification(t *testing.T) {
	httpErr := func(code int) error { return &githubx.HTTPStatusError{StatusCode: code} }
	cases := []struct {
		name string
		kind string
		err  error
		want bool
	}{
		{"dependabot 400 不可用", store.AlertKindDependabot, httpErr(400), true},
		{"dependabot 500 临时故障", store.AlertKindDependabot, httpErr(500), false},
		{"code_scanning 403 不可用", store.AlertKindCodeScanning, httpErr(403), true},
		{"code_scanning 404 不可用", store.AlertKindCodeScanning, httpErr(404), true},
		{"code_scanning 500 临时故障", store.AlertKindCodeScanning, httpErr(500), false},
		{"secret_scanning 404 不可用", store.AlertKindSecretScanning, httpErr(404), true},
		{"secret_scanning 403 临时故障", store.AlertKindSecretScanning, httpErr(403), false},
		{"非 HTTP 错误临时故障", store.AlertKindDependabot, fmt.Errorf("boom"), false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := alertUnavailable(c.kind, c.err); got != c.want {
				t.Fatalf("alertUnavailable(%s, %v) = %v, want %v", c.kind, c.err, got, c.want)
			}
		})
	}
}

// 全局 issues 与 pull_requests 开关全关：不请求 Issues API，且不推进 issues 游标。
// 游标保留旧值，重新开启后增量同步仍能拉回关窗期内的变更（防数据缺口回归）。
func TestReconcileSkipsCursorWhenIssuesAndPRsDisabled(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	off, _ := json.Marshal(false)
	for _, key := range []string{store.SettingFeatureIssues, store.SettingFeaturePullRequests} {
		if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
			ID: "feat-" + key, Key: key, ValueJSON: off,
			UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
		}); err != nil {
			t.Fatal(err)
		}
	}
	// Issues API 有数据返回，用于证明「未请求」而非「请求了但没数据」。
	fake.issuesFn = func(page int) any {
		return []map[string]any{{
			"number": 1, "state": "open", "title": "不应被采集",
			"html_url":   "https://github.com/acme/demo/issues/1",
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"user":       map[string]any{"login": "alice"},
			"labels":     []any{}, "assignees": []any{},
		}}
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	if got := fake.issuesPages.Load(); got != 0 {
		t.Fatalf("全局 issues/PR 关闭不应请求 issues API，got %d", got)
	}
	if _, err := data.Cursors().Get(ctx, repo.ID, "issues"); err == nil {
		t.Fatal("issues/PR 全关时不应推进 issues 游标")
	}
	// 仓库最后同步时间仍应刷新（对账确实执行过）。
	got, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncedAt == nil {
		t.Fatal("last_synced_at 应被更新")
	}
}

// 监控总开关关闭的仓库：整仓跳过对账，不产生任何 GitHub 请求。
func TestReconcileSkipsMonitorDisabledRepo(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	off := false
	if err := data.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{MonitorEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	repo, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("关监控的仓库应静默跳过: %v", err)
	}
	if got := fake.issuesPages.Load(); got != 0 {
		t.Fatalf("关监控不应请求 issues，got %d", got)
	}
	if got := fake.actionsPages.Load(); got != 0 {
		t.Fatalf("关监控不应请求 actions runs，got %d", got)
	}
}

// GitHub 侧已归档（is_archived=true 但状态未联动）的历史不一致仓库：
// 对账轮顺手收口归档并跳过采集。
func TestReconcileAllConvergesArchivedRepo(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	// 模拟历史不一致：is_archived=true 但 sync_status 仍 active、开关全开（Upsert 保留开关）。
	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: repo.Owner, Name: repo.Name, FullName: repo.FullName,
		InstallationID: repo.InstallationID, IsArchived: true,
		HTMLURL: repo.HTMLURL,
	}); err != nil {
		t.Fatal(err)
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileAll(ctx, 10); err != nil {
		t.Fatalf("ReconcileAll: %v", err)
	}
	got, err := data.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusArchived {
		t.Fatalf("归档仓应收口为 archived，got %s", got.SyncStatus)
	}
	if got.MonitorEnabled || got.ActionsEnabled {
		t.Fatalf("归档联动应关闭开关: %+v", got)
	}
	if got := fake.issuesPages.Load(); got != 0 {
		t.Fatalf("归档仓不应再采集，got issues 请求 %d 次", got)
	}
}

func TestReconcilePREnrichBudget(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	updated := time.Now().UTC().Truncate(time.Second)

	fake.issuesFn = func(page int) any {
		items := make([]map[string]any, 0, 30)
		for i := 0; i < 30; i++ {
			n := 100 + i
			items = append(items, map[string]any{
				"number": n, "state": "open", "title": fmt.Sprintf("PR %d", n),
				"html_url":     fmt.Sprintf("https://github.com/acme/demo/pull/%d", n),
				"pull_request": map[string]any{"url": fmt.Sprintf("https://api.github.com/repos/acme/demo/pulls/%d", n)},
				"updated_at":   updated.Format(time.RFC3339),
				"user":         map[string]any{"login": "bob"},
				"labels":       []any{}, "assignees": []any{},
			})
		}
		return items
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	// 预算是防打爆 5000 次/时配额的关键约束，回归会直接放大 API 消耗 4 倍。
	if got := fake.prDetailRequests.Load(); got > prEnrichBudgetPerRound {
		t.Fatalf("PR enrich 超出单轮预算：详情请求 %d 次 > 预算 %d", got, prEnrichBudgetPerRound)
	}
	if got := fake.prDetailRequests.Load(); got != prEnrichBudgetPerRound {
		t.Fatalf("30 个新 PR 应恰好耗尽 %d 次预算，实际 %d 次", prEnrichBudgetPerRound, got)
	}
	// 预算外的 PR 不 enrich，但基础字段必须照常落库。
	_, pageResult, err := data.WorkItems().List(ctx, store.ListFilter{Page: 1, PerPage: 50, RepositoryID: repo.ID})
	if err != nil {
		t.Fatal(err)
	}
	if pageResult.Total != 30 {
		t.Fatalf("30 个 PR 应全部落库，total=%d", pageResult.Total)
	}
}

// addSecondRepo 在测试 Store 中追加同一 installation 下的第二个仓库。
func addSecondRepo(t *testing.T, data store.Store, first store.Repository) store.Repository {
	t.Helper()
	second, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: first.Owner, Name: first.Name + "2", FullName: first.FullName + "2",
		InstallationID: first.InstallationID, HTMLURL: first.HTMLURL,
	})
	if err != nil {
		t.Fatal(err)
	}
	return second
}

// 限流（Retry-After 超出等待预算）必须立即停止本轮：
// 不再对剩余候选仓库发起请求，避免逐个仓库连环请求放大次限流。
// 回归：原先限流被当单仓故障记日志后继续打 API，几十个仓库各吃一次 429。
func TestReconcileAllStopsRoundOnRateLimit(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	addSecondRepo(t, data, repo)

	fake.rateLimitIssuesRequests = 1 << 30 // 持续限流
	fake.issues429RetryAfter = "3600"      // 超过 maxRateLimitWait（2 分钟）→ 直接停止本轮

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileAll(ctx, 10); err != nil {
		t.Fatalf("限流停止本轮不应视为失败: %v", err)
	}
	if got := fake.issuesPages.Load(); got != 1 {
		t.Fatalf("限流后应只请求 1 个仓库，实际 %d 次", got)
	}
}

// 限流等待时长在预算内：等待上游建议时长后继续处理剩余候选，整轮不因单仓限流夭折。
func TestReconcileAllWaitsAndContinuesOnRateLimit(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()
	second := addSecondRepo(t, data, repo)

	oldBudget := maxRateLimitWait
	maxRateLimitWait = 2 * time.Second
	t.Cleanup(func() { maxRateLimitWait = oldBudget })

	fake.rateLimitIssuesRequests = 1 // 仅第一个请求限流
	fake.issues429RetryAfter = "1"

	r := &Reconciler{Store: data, GitHub: fake.client}
	start := time.Now()
	if err := r.ReconcileAll(ctx, 10); err != nil {
		t.Fatalf("等待后续航不应失败: %v", err)
	}
	if elapsed := time.Since(start); elapsed < 900*time.Millisecond {
		t.Fatalf("应等待上游建议的 1s 再继续，实际耗时 %v", elapsed)
	}
	if got := fake.issuesPages.Load(); got != 2 {
		t.Fatalf("等待后应继续对账第二个仓库，实际请求 %d 次", got)
	}
	// 被限流的仓库不刷新同步时间，另一仓库正常落库。
	synced := 0
	for _, id := range []string{repo.ID, second.ID} {
		got, err := data.Repositories().Get(ctx, id)
		if err != nil {
			t.Fatal(err)
		}
		if got.LastSyncedAt != nil {
			synced++
		}
	}
	if synced != 1 {
		t.Fatalf("限流仓不刷新、另一仓刷新，期望恰好 1 个仓库同步，实际 %d", synced)
	}
}

// 告警对账路径的限流不能被软失败吞掉：必须向上传播终止本轮，
// 否则 ReconcileAll 会继续对下一仓库发起请求（次限流是全安装级信号）。
func TestReconcileAlertsRateLimitPropagates(t *testing.T) {
	data, fake, repo := newReconcileFixture(t)
	ctx := t.Context()

	fake.alertHTTPStatus = map[string]int{"code_scanning": http.StatusTooManyRequests}
	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); !githubx.IsRateLimited(err) {
		t.Fatalf("告警路径限流应向上返回，got %v", err)
	}
}

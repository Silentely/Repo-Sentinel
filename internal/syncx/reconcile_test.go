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
	// dependabotFn 按页返回 dependabot alerts 响应体；用例可定制，默认空页。
	dependabotFn func(page int) any

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
	path := r.URL.Path
	switch {
	case reAccessTokens.MatchString(path):
		// Installation Token 签发：任意 JWT 都接受，直接返回测试 token。
		write(map[string]any{"token": "test-installation-token", "expires_at": time.Now().Add(time.Hour).UTC().Format(time.RFC3339)})
	case reIssues.MatchString(path):
		f.issuesPages.Add(1)
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
			write(f.dependabotFn(page))
			return
		}
		write([]any{})
	case reCodeScanning.MatchString(path) || reSecretScan.MatchString(path):
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

// TestReconcileDependabotAlertsPagination 验证告警多页拉取：
// 第一页满 50 条时必须继续拉第二页，51 条告警须全部落库（防回归"只拉第一页"）。
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
	fake.dependabotFn = func(page int) any {
		switch page {
		case 1:
			items := make([]map[string]any, 0, 50)
			for i := 1; i <= 50; i++ {
				items = append(items, alert(i))
			}
			return items
		case 2:
			return []map[string]any{alert(51)}
		default:
			return []any{}
		}
	}

	r := &Reconciler{Store: data, GitHub: fake.client}
	if err := r.ReconcileRepository(ctx, repo); err != nil {
		t.Fatalf("对账应成功: %v", err)
	}
	if got := fake.dependabotPages.Load(); got < 2 {
		t.Fatalf("第一页满 50 条时应继续拉第二页，实际请求 %d 次", got)
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

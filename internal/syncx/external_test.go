package syncx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func openSyncStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "sync.db"),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func TestExternalPollBaselineDoesNotCreateEvents(t *testing.T) {
	data := openSyncStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number":     1,
				"state":      "open",
				"title":      "baseline issue",
				"html_url":   "https://github.com/acme/demo/issues/1",
				"updated_at": time.Now().UTC().Format(time.RFC3339),
				"user":       map[string]any{"login": "alice"},
				"labels":     []any{},
				"assignees":  []any{},
			},
		})
	}))
	t.Cleanup(srv.Close)

	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
		SyncStatus: store.SyncStatusBaseline, Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &ExternalPoller{
		Store:  data,
		Client: &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	events, _, err := data.Events().List(t.Context(), store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("基线不应产生事件，got %d", len(events))
	}
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.SyncStatus != store.SyncStatusActive {
		t.Fatalf("基线后应 active，got %s", got.SyncStatus)
	}
}

func TestExternalPollActiveCreatesEventOnChange(t *testing.T) {
	data := openSyncStore(t)
	updated := time.Now().UTC().Truncate(time.Second)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode([]map[string]any{
			{
				"number":     7,
				"state":      "open",
				"title":      "active issue",
				"html_url":   "https://github.com/acme/demo/issues/7",
				"updated_at": updated.Format(time.RFC3339),
				"user":       map[string]any{"login": "bob"},
				"labels":     []any{},
				"assignees":  []any{},
			},
		})
	}))
	t.Cleanup(srv.Close)

	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
		SyncStatus: store.SyncStatusActive, Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	p := &ExternalPoller{
		Store:  data,
		Client: &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	events, _, err := data.Events().List(t.Context(), store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 1 {
		t.Fatalf("active 变更应 1 事件，got %d", len(events))
	}
	// 再次轮询同状态：不应重复事件（指纹幂等）
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatal(err)
	}
	events2, _, err := data.Events().List(t.Context(), store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(events2) != 1 {
		t.Fatalf("重复轮询不应新增事件，got %d", len(events2))
	}
}

// 监控关闭或已归档的外部仓：轮询直接跳过，不请求 GitHub、不写数据。
func TestExternalPollSkipsGatedRepos(t *testing.T) {
	data := openSyncStore(t)
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	t.Cleanup(srv.Close)

	disabled, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
		SyncStatus: store.SyncStatusActive, Owner: "acme", Name: "off", FullName: "acme/off",
		HTMLURL: "https://github.com/acme/off",
	})
	if err != nil {
		t.Fatal(err)
	}
	off := false
	if err := data.Repositories().UpdateSettings(t.Context(), disabled.ID, store.RepositorySettings{MonitorEnabled: &off}); err != nil {
		t.Fatal(err)
	}
	disabled, err = data.Repositories().Get(t.Context(), disabled.ID)
	if err != nil {
		t.Fatal(err)
	}

	archived, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
		SyncStatus: store.SyncStatusArchived, Owner: "acme", Name: "arch", FullName: "acme/arch",
		IsArchived: true, HTMLURL: "https://github.com/acme/arch",
	})
	if err != nil {
		t.Fatal(err)
	}

	p := &ExternalPoller{
		Store:  data,
		Client: &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	if err := p.PollOne(t.Context(), disabled); err != nil {
		t.Fatal(err)
	}
	if err := p.PollOne(t.Context(), archived); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 0 {
		t.Fatalf("被门禁拦截的仓库不应发起请求，got %d", got)
	}
}

func TestReconcileRequiresGitHubApp(t *testing.T) {
	data := openSyncStore(t)
	r := &Reconciler{Store: data, GitHub: nil}
	inst := "1"
	// 监控开关必须显式开启：能力门禁优先于 App 配置检查。
	err := r.ReconcileRepository(t.Context(), store.Repository{
		ID: "x", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "a", Name: "b", FullName: "a/b",
		MonitorEnabled: true,
		InstallationID: &inst,
	})
	if err == nil {
		t.Fatal("未配置 App 应失败")
	}
}

// externalRepoFixture 构造外部仓（acme/demo）与按路径分发的伪 GitHub 服务器：
// /repos/acme/demo/issues 返回空 issues，/repos/acme/demo 返回可配置的仓库元数据。
func externalRepoFixture(t *testing.T, repoMetaFn func(http.ResponseWriter)) (store.Store, store.Repository, *githubx.PublicClient) {
	t.Helper()
	data := openSyncStore(t)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/repos/acme/demo/issues":
			_ = json.NewEncoder(w).Encode([]any{})
		case "/repos/acme/demo":
			repoMetaFn(w)
		default:
			http.Error(w, "unexpected path: "+r.URL.Path, http.StatusInternalServerError)
		}
	}))
	t.Cleanup(srv.Close)

	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
		SyncStatus: store.SyncStatusActive, Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	return data, repo, &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()}
}

// 快照正路径：外部仓轮询时 GetRepository 成功 → stargazers_count>0 → Upsert 落库，
// sample_date 为当日 UTC 日期。
func TestExternalPollWritesStarSnapshot(t *testing.T) {
	data, repo, client := externalRepoFixture(t, func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(map[string]any{"stargazers_count": 456, "forks_count": 10, "open_issues_count": 3})
	})
	p := &ExternalPoller{Store: data, Client: client}
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatal(err)
	}

	rows, err := data.RepoStatSnapshots().ListInRange(t.Context(), []string{repo.ID}, "stargazers", "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 {
		t.Fatalf("应落 1 条 stargazers 快照，got %d", len(rows))
	}
	if rows[0].RepositoryID != repo.ID {
		t.Fatalf("快照应归属仓库 %s，got %s", repo.ID, rows[0].RepositoryID)
	}
	if rows[0].Value != 456 {
		t.Fatalf("快照值应为 456，got %d", rows[0].Value)
	}
	if want := time.Now().UTC().Format("2006-01-02"); rows[0].SampleDate != want {
		t.Fatalf("快照 sample_date 应为当日 UTC %s，got %s", want, rows[0].SampleDate)
	}
}

// 0 值守卫：stargazers_count=0 时不落快照。
func TestExternalPollSkipsZeroStarSnapshot(t *testing.T) {
	data, repo, client := externalRepoFixture(t, func(w http.ResponseWriter) {
		_ = json.NewEncoder(w).Encode(map[string]any{"stargazers_count": 0})
	})
	p := &ExternalPoller{Store: data, Client: client}
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatal(err)
	}

	rows, err := data.RepoStatSnapshots().ListInRange(t.Context(), []string{repo.ID}, "stargazers", "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("stargazers_count=0 不应落快照，got %d 行", len(rows))
	}
}

// 软失败路径：元数据端点 500 时轮询不阻断（主流程成功推进）、不落快照，
// 仅记录 star_snapshot_failed 日志。
func TestExternalPollStarSnapshotSoftFail(t *testing.T) {
	data, repo, client := externalRepoFixture(t, func(w http.ResponseWriter) {
		http.Error(w, http.StatusText(http.StatusInternalServerError), http.StatusInternalServerError)
	})
	var buf bytes.Buffer
	p := &ExternalPoller{
		Store:  data,
		Client: client,
		Logger: slog.New(slog.NewTextHandler(&buf, nil)),
	}
	if err := p.PollOne(t.Context(), repo); err != nil {
		t.Fatalf("快照软失败不应阻断轮询: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("star_snapshot_failed")) {
		t.Fatalf("应记录 star_snapshot_failed 软失败日志，got %q", buf.String())
	}
	// 主流程仍成功：仓库同步时间被刷新。
	got, err := data.Repositories().Get(t.Context(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.LastSyncedAt == nil {
		t.Fatal("轮询主流程应成功推进 last_synced_at")
	}
	rows, err := data.RepoStatSnapshots().ListInRange(t.Context(), []string{repo.ID}, "stargazers", "2000-01-01", "2100-01-01")
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("软失败不应落快照，got %d 行", len(rows))
	}
}

// pollAllFixture 建 n 个外部仓（acme/r0…）与记录并发度的伪 GitHub 服务：
// issues 路径可注入额外延迟以便 worker 重叠，仓库元数据返回 0 star（不落快照）。
// 返回 store、记录器（线程安全）与统计已轮询仓数的探针。
func pollAllFixture(t *testing.T, n int, issueDelay time.Duration, issuesFn func(w http.ResponseWriter, name string)) (store.Store, *pollRecorder, *githubx.PublicClient) {
	t.Helper()
	data := openSyncStore(t)
	rec := &pollRecorder{mu: &sync.Mutex{}, polled: map[string]int{}}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		rec.enter()
		defer rec.leave()
		name := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/repos/acme/"), "/issues")
		if !strings.HasSuffix(r.URL.Path, "/issues") {
			name = ""
		}
		if strings.HasSuffix(r.URL.Path, "/issues") {
			rec.markPolled(name)
		}
		if issueDelay > 0 && strings.HasSuffix(r.URL.Path, "/issues") {
			time.Sleep(issueDelay)
		}
		if strings.HasSuffix(r.URL.Path, "/issues") {
			if issuesFn != nil {
				issuesFn(w, name)
				return
			}
			_ = json.NewEncoder(w).Encode([]any{})
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"stargazers_count": 0})
	}))
	t.Cleanup(srv.Close)
	for i := 0; i < n; i++ {
		if _, err := data.Repositories().Upsert(t.Context(), store.Repository{
			ID: ulid.Make().String(), Type: store.RepositoryTypeExternal,
			SyncStatus: store.SyncStatusActive, Owner: "acme", Name: fmt.Sprintf("r%d", i),
			FullName: fmt.Sprintf("acme/r%d", i), HTMLURL: fmt.Sprintf("https://github.com/acme/r%d", i),
		}); err != nil {
			t.Fatal(err)
		}
	}
	return data, rec, &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()}
}

// pollRecorder 线程安全地记录请求在途数、在途峰值与被轮询过的仓库名。
type pollRecorder struct {
	mu       *sync.Mutex
	inflight int
	maxSeen  int
	polled   map[string]int
}

func (r *pollRecorder) enter() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflight++
	if r.inflight > r.maxSeen {
		r.maxSeen = r.inflight
	}
}

func (r *pollRecorder) leave() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.inflight--
}

func (r *pollRecorder) markPolled(name string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.polled[name]++
}

// polledCount 返回已轮询过的不同仓库数与峰值并发（读取后清零计数，便于分批断言）。
func (r *pollRecorder) snapshot() (repos int, peak int) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.polled), r.maxSeen
}

// PollAll 并发有界：仓数超过 worker 上限时在途请求不得超过 externalPollConcurrency，
// 且每个候选仓都被轮询到（一轮完整跑完）。
func TestExternalPollAllBoundsConcurrency(t *testing.T) {
	const repoCount = externalPollConcurrency * 2
	data, rec, client := pollAllFixture(t, repoCount, 40*time.Millisecond, nil)

	p := &ExternalPoller{Store: data, Client: client}
	if err := p.PollAll(t.Context()); err != nil {
		t.Fatalf("PollAll failed: %v", err)
	}
	polled, peak := rec.snapshot()
	if polled != repoCount {
		t.Fatalf("应轮询全部 %d 个仓，got %d", repoCount, polled)
	}
	if peak <= 0 {
		t.Fatal("未观察到任何请求")
	}
	if peak > externalPollConcurrency {
		t.Fatalf("并发度应不超过 %d，got %d", externalPollConcurrency, peak)
	}
	if peak == 1 {
		t.Fatal("并发未生效（峰值并发为 1）")
	}
	// 整轮跑完：每个仓的同步时间都被推进。
	repos, err := data.Repositories().ListSyncCandidates(t.Context(), store.RepositoryTypeExternal, store.MaxExternalRepositories)
	if err != nil {
		t.Fatal(err)
	}
	for _, repo := range repos {
		if repo.LastSyncedAt == nil {
			t.Fatalf("%s 的 last_synced_at 应被推进", repo.FullName)
		}
	}
}

// 非限流失败只记单仓错误，不该中止本轮：其余仓仍应被轮询到。
func TestExternalPollAllContinuesAfterRepoFailure(t *testing.T) {
	const repoCount = 6
	data, rec, client := pollAllFixture(t, repoCount, 0, func(w http.ResponseWriter, name string) {
		if name == "r0" {
			http.Error(w, "boom", http.StatusInternalServerError)
			return
		}
		_ = json.NewEncoder(w).Encode([]any{})
	})
	var buf bytes.Buffer
	p := &ExternalPoller{Store: data, Client: client, Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	if err := p.PollAll(t.Context()); err != nil {
		t.Fatalf("PollAll failed: %v", err)
	}
	polled, _ := rec.snapshot()
	if polled != repoCount {
		t.Fatalf("单仓失败不应中止本轮，应轮询 %d 个仓，got %d", repoCount, polled)
	}
	logs := buf.String()
	if !strings.Contains(logs, "external_poll_failed") || !strings.Contains(logs, "acme/r0") {
		t.Fatalf("应记录 r0 的失败日志，got %q", logs)
	}
	if strings.Contains(logs, "rate_limited_round_stopped") {
		t.Fatalf("非限流失败不应记限流日志，got %q", logs)
	}
}

// 限流是令牌级信号：命中后本轮不再启动新的仓，且多个仓同时限流也只记一次告警。
func TestExternalPollAllStopsRoundOnRateLimit(t *testing.T) {
	const repoCount = externalPollConcurrency * 2
	data, rec, client := pollAllFixture(t, repoCount, 40*time.Millisecond, func(w http.ResponseWriter, name string) {
		w.Header().Set("Retry-After", "60")
		w.WriteHeader(http.StatusTooManyRequests)
	})
	var buf bytes.Buffer
	p := &ExternalPoller{Store: data, Client: client, Logger: slog.New(slog.NewTextHandler(&buf, nil))}
	if err := p.PollAll(t.Context()); err != nil {
		t.Fatalf("限流不应向上抛错，got %v", err)
	}
	if _, peak := rec.snapshot(); peak > externalPollConcurrency {
		t.Fatalf("并发度应不超过 %d，got %d", externalPollConcurrency, peak)
	}
	logs := buf.String()
	if got := strings.Count(logs, "rate_limited_round_stopped"); got != 1 {
		t.Fatalf("限流告警应只记一次，got %d 次：%q", got, logs)
	}
}

// 派发前即取消：整轮不发起任何外部请求，由存储层在拉取候选阶段快速失败并向上传播。
// 注意断言范围仅限「前置取消」——已在途的请求会跑完并落库（见
// TestExternalPollAllPropagatesContextCancelDuringPolling），两者共同定义「取消即止」语义。
func TestExternalPollAllRespectsContextCancel(t *testing.T) {
	data, rec, client := pollAllFixture(t, externalPollConcurrency*2, 0, nil)
	p := &ExternalPoller{Store: data, Client: client}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	err := p.PollAll(ctx)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("取消上下文应快速失败，got %v", err)
	}
	if polled, _ := rec.snapshot(); polled != 0 {
		t.Fatalf("前置取消不应发起任何仓的请求，got %d", polled)
	}
}

// 上下文在候选仓加载完成、worker 已开始请求后取消时，整轮应向上传播取消错误，
// 不能把提前结束误报为成功；此时已派发的请求会跑完（不做回滚）。
func TestExternalPollAllPropagatesContextCancelDuringPolling(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	var cancelOnce sync.Once
	data, _, client := pollAllFixture(t, externalPollConcurrency*2, 0, func(w http.ResponseWriter, _ string) {
		cancelOnce.Do(cancel)
		_ = json.NewEncoder(w).Encode([]any{})
	})

	p := &ExternalPoller{Store: data, Client: client}
	if err := p.PollAll(ctx); !errors.Is(err, context.Canceled) {
		t.Fatalf("轮询期间取消上下文应向上传播，got %v", err)
	}
}

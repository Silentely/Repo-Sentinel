package syncx

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
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

package syncx

import (
	"encoding/json"
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

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
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// starredTestEnv 组装 mock GitHub + 测试 Poller，响应可编程。
type starredTestEnv struct {
	data   store.Store
	poller *StarredReleasePoller

	mu        sync.Mutex
	starred   map[int][]map[string]any // page → 公开 star 条目
	releases  map[string][]map[string]any
	rateLimit bool // 触发匿名枚举限流
}

func newStarredTestEnv(t *testing.T, setup func(env *starredTestEnv)) *starredTestEnv {
	t.Helper()
	data := openSyncStore(t)
	env := &starredTestEnv{
		data:     data,
		starred:  map[int][]map[string]any{},
		releases: map[string][]map[string]any{},
	}
	now := time.Now().UTC()
	if _, err := data.Installations().Upsert(t.Context(), store.GitHubInstallation{
		ID: ulid.Make().String(), InstallationID: 1, AccountLogin: "octocat",
		CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: SettingStarredUsername,
		ValueJSON: json.RawMessage(`"octocat"`), UpdatedAt: now, UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	setup(env)

	key, _ := rsa.GenerateKey(rand.Reader, 2048)
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		env.mu.Lock()
		defer env.mu.Unlock()
		w.Header().Set("X-RateLimit-Remaining", "100")
		switch {
		case r.Method == http.MethodPost && strings.HasPrefix(r.URL.Path, "/app/installations/"):
			_ = json.NewEncoder(w).Encode(map[string]any{
				"token": "inst-tok-1", "expires_at": time.Now().UTC().Add(time.Hour).Format(time.RFC3339),
			})
		case strings.HasPrefix(r.URL.Path, "/users/") && strings.HasSuffix(r.URL.Path, "/starred"):
			if env.rateLimit {
				w.WriteHeader(http.StatusTooManyRequests)
				return
			}
			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				fmt.Sscanf(p, "%d", &page)
			}
			items := env.starred[page]
			if items == nil {
				items = []map[string]any{}
			}
			if page == 1 && len(env.starred[2]) > 0 {
				w.Header().Set("Link", `<https://api.github.com/user/starred?page=2>; rel="next"`)
			}
			_ = json.NewEncoder(w).Encode(items)
		case strings.HasPrefix(r.URL.Path, "/repos/"):
			rest := strings.TrimPrefix(r.URL.Path, "/repos/")
			parts := strings.SplitN(rest, "/", 2)
			if len(parts) != 2 {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			fullName := parts[0] + "/" + strings.SplitN(parts[1], "/", 2)[0]
			items := env.releases[fullName]
			if items == nil {
				w.WriteHeader(http.StatusNotFound)
				return
			}
			w.Header().Set("ETag", `"e1"`)
			_ = json.NewEncoder(w).Encode(items)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	env.poller = &StarredReleasePoller{
		Store:  data,
		GitHub: &githubx.AppClient{AppID: 1, PrivateKeyPEM: string(pemBytes), BaseURL: srv.URL, HTTP: srv.Client()},
		Public: &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()},
	}
	return env
}

func releaseMap(id int64, tag string, prerelease bool) map[string]any {
	return map[string]any{
		"id": id, "tag_name": tag, "name": "Rel " + tag, "draft": false, "prerelease": prerelease,
		"published_at": time.Now().UTC().Format(time.RFC3339),
		"html_url":     "https://github.com/x/y/releases/tag/" + tag,
		"body":         "notes for " + tag,
		"author":       map[string]any{"login": "octo"},
	}
}

func listReleaseEvents(t *testing.T, data store.Store) []store.Event {
	t.Helper()
	events, _, err := data.Events().List(t.Context(), store.ListFilter{Page: 1, PerPage: 50, Kind: store.ReleaseKind})
	if err != nil {
		t.Fatal(err)
	}
	return events
}

// TestStarredSyncStars_注册过滤与基线 覆盖 fork/archived 预过滤、无 release → inactive、基线不建事件。
func TestStarredSyncStars_注册过滤与基线(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{
			{"full_name": "octocat/Hello-World", "fork": false, "archived": false},
			{"full_name": "o/forked", "fork": true, "archived": false},
			{"full_name": "o/archived", "fork": false, "archived": true},
		}
		env.starred[2] = []map[string]any{
			{"full_name": "acme/norelease", "fork": false, "archived": false},
		}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
		env.releases["acme/norelease"] = []map[string]any{}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := env.data.StarredTrackers().GetByFullName(ctx, "o/forked"); err != store.ErrNotFound {
		t.Fatalf("fork 仓不应注册: %v", err)
	}
	if _, err := env.data.StarredTrackers().GetByFullName(ctx, "o/archived"); err != store.ErrNotFound {
		t.Fatalf("archived 仓不应注册: %v", err)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking || tk.LastReleaseID != 42 {
		t.Fatalf("有 release 仓应 tracking 且记基线: %+v %v", tk, err)
	}
	no, err := env.data.StarredTrackers().GetByFullName(ctx, "acme/norelease")
	if err != nil || no.State != store.TrackerStateInactive || no.NoReleaseRecheckAt == nil {
		t.Fatalf("无 release 仓应 inactive 且带复查时间: %+v %v", no, err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != 0 {
		t.Fatalf("基线不应产生事件，got %d", len(events))
	}
}

// TestStarredSyncStars_限流不移除 验证匿名限流时中止本轮且不执行 unstar 移除。
func TestStarredSyncStars_限流不移除(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 用户取消 star：star 列表不再包含该仓，且枚举限流 → 不能误删追踪。
	env.mu.Lock()
	env.rateLimit = true
	env.mu.Unlock()
	env.poller.lastStarSync = time.Time{}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking {
		t.Fatalf("限流中止时不应移除追踪: %+v %v", tk, err)
	}
}

// TestStarredSyncStars_unstar停用 验证完整拉全后不在列表的 tracking 仓被停用。
func TestStarredSyncStars_unstar停用(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	env.starred[1] = []map[string]any{} // 用户 unstar
	env.mu.Unlock()
	env.poller.lastStarSync = time.Time{}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateDisabled {
		t.Fatalf("unstar 后应停用: %+v %v", tk, err)
	}
}

// TestStarredSyncStarsNow_绕过周期 验证 HTTP「立即同步」不受定时周期自判拦截。
func TestStarredSyncStarsNow_绕过周期(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 用户 star 了新仓；周期未到（lastStarSync 刚设）→ 常规 SyncStars 应被静默拦截。
	env.mu.Lock()
	env.starred[1] = append(env.starred[1], map[string]any{"full_name": "acme/newrepo", "fork": false, "archived": false})
	env.releases["acme/newrepo"] = []map[string]any{releaseMap(7, "v0.1", false)}
	env.mu.Unlock()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	if _, err := env.data.StarredTrackers().GetByFullName(ctx, "acme/newrepo"); err != store.ErrNotFound {
		t.Fatalf("周期内常规 SyncStars 应被拦截（不注册新仓）: %v", err)
	}
	// 立即同步（SyncStarsNow）应绕过周期强制执行。
	if err := env.poller.SyncStarsNow(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "acme/newrepo")
	if err != nil || tk.State != store.TrackerStateTracking {
		t.Fatalf("SyncStarsNow 应绕过周期注册新仓: %+v %v", tk, err)
	}
}

// TestStarredPollReleases_新release事件与幂等 覆盖事件创建、游标推进与重复轮询幂等。
func TestStarredPollReleases_新release事件与幂等(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 发布 v2.0（新 release id）
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(43, "v2.0", false)}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	events := listReleaseEvents(t, env.data)
	if len(events) != 1 || events[0].SubjectNumber == nil || *events[0].SubjectNumber != 43 {
		t.Fatalf("应产生 1 条新 release 事件: %+v", events)
	}
	if events[0].HTMLURL == "" || events[0].PayloadSummary["tag_name"] != "v2.0" {
		t.Fatalf("事件应带链接与 tag: %+v", events[0])
	}
	// 幂等：同 release 再次轮询不新增事件
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != 1 {
		t.Fatalf("重复轮询不应新增事件，got %d", len(events))
	}
}

// TestStarredPollReleases_基线不建事件 验证 SyncStars 基线后轮询不产生历史洪泛。
func TestStarredPollReleases_基线不建事件(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != 0 {
		t.Fatalf("基线不应建事件，got %d", len(events))
	}
}

// TestStarredPollReleases_预发布过滤 验证 prerelease 默认不通知但推进游标。
func TestStarredPollReleases_预发布过滤(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(43, "v2.0-rc1", true)}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != 0 {
		t.Fatalf("预发布默认不通知，got %d", len(events))
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.LastReleaseID != 43 {
		t.Fatalf("预发布应推进游标: %+v", tk)
	}
}

// TestStarredPollReleases_空列表inactive 验证轮询发现 release 列表为空时转入 inactive。
func TestStarredPollReleases_空列表inactive(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.State != store.TrackerStateInactive {
		t.Fatalf("release 列表为空应转 inactive: %+v", tk)
	}
}

// TestStarredPollReleases_404不可用 验证删仓/转私有标记 unavailable。
func TestStarredPollReleases_404不可用(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	delete(env.releases, "octocat/Hello-World") // mock 返回 404
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.State != store.TrackerStateUnavailable {
		t.Fatalf("404 应标记 unavailable: %+v", tk)
	}
}

// TestStarredPollReleases_功能开关关闭 验证全局开关关闭时不轮询不建事件。
func TestStarredPollReleases_功能开关关闭(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(false)
	if _, err := env.data.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: store.SettingFeatureStarredReleases,
		ValueJSON: raw, UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(43, "v2.0", false)}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != 0 {
		t.Fatalf("开关关闭不应建事件，got %d", len(events))
	}
}

// TestStarredPollReleases_并发安全 验证 Scheduler 节拍与 HTTP 立即同步并发触发时无竞态。
func TestStarredPollReleases_并发安全(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	var wg sync.WaitGroup
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = env.poller.SyncStars(ctx)
			_ = env.poller.PollReleases(ctx)
		}()
	}
	wg.Wait()
	// 并发下不 panic、不产生重复事件（Upsert/幂等兜底）。
	events := listReleaseEvents(t, env.data)
	if len(events) != 0 {
		t.Fatalf("基线并发不应建事件，got %d", len(events))
	}
}

// TestStarredPollReleases_通知链路 验证新 release 事件经 rules.Engine 写入 Outbox（实时通知）。
func TestStarredPollReleases_通知链路(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	// 启用渠道（EventKinds 为 nil = 全部订阅）。
	if _, err := env.data.Channels().Upsert(ctx, store.NotificationChannel{
		ID: ulid.Make().String(), ChannelType: store.ChannelTelegram, Name: "tg", Target: "123",
		Enabled: true, CreatedAt: time.Now().UTC(), UpdatedAt: time.Now().UTC(),
	}); err != nil {
		t.Fatal(err)
	}
	env.poller.Engine = &rules.Engine{Store: env.data}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 发布 v2.0（新 release id）→ 应产生事件且写入 Outbox。
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(43, "v2.0", false)}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	outbox, _, err := env.data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	if len(outbox) != 1 {
		t.Fatalf("新 release 应写入 1 条 outbox，got %d", len(outbox))
	}
	if !strings.Contains(outbox[0].Title, "🚀") || !strings.Contains(outbox[0].Title, "新版本发布") {
		t.Fatalf("outbox 标题应为 release 通知文案: %s", outbox[0].Title)
	}
	// 幂等：重复轮询同 release 不重复投递。
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	outbox, _, _ = env.data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if len(outbox) != 1 {
		t.Fatalf("重复轮询不应重复投递，got %d", len(outbox))
	}
}

package syncx

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"log/slog"
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

	// starredPages 记录 star 列表分页请求的页码序列，供断言翻页终止行为。
	starredPages []int

	// starNextForever 模拟异常上游：越界空页也持续下发 rel="next"，
	// 用于验证页码防御上限生效（不会无限翻页）。
	starNextForever bool

	// releaseUnchanged 标记 release 未变化：条件请求命中 If-None-Match 时返回 304（模拟 GitHub）。
	releaseUnchanged map[string]bool
}

func newStarredTestEnv(t *testing.T, setup func(env *starredTestEnv)) *starredTestEnv {
	t.Helper()
	data := openSyncStore(t)
	env := &starredTestEnv{
		data:             data,
		starred:          map[int][]map[string]any{},
		releases:         map[string][]map[string]any{},
		releaseUnchanged: map[string]bool{},
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
	if setup != nil {
		setup(env)
	}

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
			env.starredPages = append(env.starredPages, page)
			// 模拟 GitHub Link 头语义（RFC 5988）：仍有数据页时下发 rel="next"；
			// 末页与越界空页只带 rel="prev"/"first"（不再带 next）。
			if link := starredLinkHeader(env.starred, page, env.starNextForever); link != "" {
				w.Header().Set("Link", link)
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
			// 模拟条件请求 304：release 未变化且 If-None-Match 命中时返回空 body 的 304。
			if env.releaseUnchanged[fullName] && r.Header.Get("If-None-Match") == `"e1"` {
				w.WriteHeader(http.StatusNotModified)
				return
			}
			// 模拟 GitHub 分页：按 per_page/page 切片，翻页返回剩余条目。
			page := 1
			if p := r.URL.Query().Get("page"); p != "" {
				fmt.Sscanf(p, "%d", &page)
			}
			perPage := 30
			if pp := r.URL.Query().Get("per_page"); pp != "" {
				fmt.Sscanf(pp, "%d", &perPage)
			}
			start := (page - 1) * perPage
			if start >= len(items) {
				items = nil
			} else if end := start + perPage; end < len(items) {
				items = items[start:end]
			} else {
				items = items[start:]
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

// starredLinkHeader 按 GitHub 真实分页语义生成 Link 头（RFC 5988）：
// 仍有数据页时下发 rel="next"；末页与越界空页只带 rel="prev"/"first"（无 next）。
// nextForever 为 true 时越界空页也持续下发 rel="next"，模拟不会自终止的异常上游，
// 用于验证页码防御上限生效。
func starredLinkHeader(starred map[int][]map[string]any, page int, nextForever bool) string {
	lastPage := 1
	for p := 1; p <= len(starred)+1; p++ {
		if len(starred[p]) > 0 {
			lastPage = p
		}
	}
	var links []string
	if page < lastPage || nextForever {
		links = append(links, fmt.Sprintf(`<https://api.github.com/user/starred?page=%d>; rel="next"`, page+1))
		links = append(links, fmt.Sprintf(`<https://api.github.com/user/starred?page=%d>; rel="last"`, lastPage+1))
	}
	if page > 1 {
		links = append(links, fmt.Sprintf(`<https://api.github.com/user/starred?page=%d>; rel="prev"`, page-1))
	}
	if lastPage > 1 {
		links = append(links, `<https://api.github.com/user/starred?page=1>; rel="first"`)
	}
	return strings.Join(links, ", ")
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
	now := time.Now().UTC()
	for _, item := range []store.NotificationOutbox{
		{ID: "ob-unstar-pending", ChannelID: "ch-1", IdempotencyKey: "idem|unstar-pending", Status: store.OutboxPending,
			NextAttemptAt: now, Title: "release", BodyText: "release", BodyJSON: map[string]any{"kind": store.ReleaseKind, "repository": "octocat/Hello-World"}},
		{ID: "ob-unstar-sent", ChannelID: "ch-1", IdempotencyKey: "idem|unstar-sent", Status: store.OutboxSent,
			NextAttemptAt: now, Title: "release", BodyText: "release", BodyJSON: map[string]any{"kind": store.ReleaseKind, "repository": "octocat/Hello-World"}},
		{ID: "ob-other-pending", ChannelID: "ch-1", IdempotencyKey: "idem|other-pending", Status: store.OutboxPending,
			NextAttemptAt: now, Title: "release", BodyText: "release", BodyJSON: map[string]any{"kind": store.ReleaseKind, "repository": "acme/other"}},
	} {
		if _, err := env.data.Outbox().Create(ctx, item); err != nil {
			t.Fatal(err)
		}
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
	outbox, _, err := env.data.Outbox().List(ctx, store.ListFilter{Page: 1, PerPage: 20})
	if err != nil {
		t.Fatal(err)
	}
	statuses := make(map[string]string, len(outbox))
	for _, item := range outbox {
		statuses[item.ID] = item.Status
	}
	if statuses["ob-unstar-pending"] != store.OutboxCancelled {
		t.Fatalf("unstar 后目标仓 pending outbox 应取消，got %q", statuses["ob-unstar-pending"])
	}
	if statuses["ob-unstar-sent"] != store.OutboxSent || statuses["ob-other-pending"] != store.OutboxPending {
		t.Fatalf("已发送或其他仓库 outbox 不应改变: %+v", statuses)
	}
}

// TestStarredLastStarSyncAt 验证同步落定时刻可观测：同步前为零值、完成后推进。
// HTTP 层据此向管理台暴露 last_star_sync_at——「立即同步」是异步执行的，
// 前端必须等该时刻推进后再刷新追踪列表，否则看到的是同步前数据。
func TestStarredLastStarSyncAt(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	if !env.poller.LastStarSyncAt().IsZero() {
		t.Fatal("同步前 lastStarSyncAt 应为零值")
	}
	if err := env.poller.SyncStarsNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	if env.poller.LastStarSyncAt().IsZero() {
		t.Fatal("同步完成后 lastStarSyncAt 应推进")
	}
	// 未配置用户名的确定性跳过同样推进记账（避免每节拍空转）。
	if _, err := env.data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: SettingStarredUsername, ValueJSON: json.RawMessage(`""`),
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.poller.SyncStarsNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	if env.poller.LastStarSyncAt().IsZero() {
		t.Fatal("确定性跳过后 lastStarSyncAt 不应归零")
	}
}

// TestStarredSyncStars_多页末页Link头unstar停用 复现「已 unstar 的仓库仍被轮询推送」：
// GitHub 多页结果的最后一页仍带 Link 头（rel="first"/"prev"，无 rel="next"）。
// 分页必须以 rel="next" 终止；若以「头非空」判断则会把末页当成中间页继续请求越界空页，
// 越界页同样带 prev/first，最终打到页码防御上限 → 完整分页标记失效 →
// removeUnstarred 被整体跳过（且无任何告警），已 unstar 的仓保持 tracking 继续轮询。
func TestStarredSyncStars_多页末页Link头unstar停用(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{
			{"full_name": "octocat/Hello-World", "fork": false, "archived": false},
		}
		env.starred[2] = []map[string]any{
			{"full_name": "octocat/gone", "fork": false, "archived": false},
			{"full_name": "octocat/keep2", "fork": false, "archived": false},
		}
		env.starred[3] = []map[string]any{
			{"full_name": "octocat/Spoon-Knife", "fork": false, "archived": false},
		}
		for _, repo := range []string{"octocat/Hello-World", "octocat/gone", "octocat/keep2", "octocat/Spoon-Knife"} {
			env.releases[repo] = []map[string]any{releaseMap(42, "v1.0", false)}
		}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 用户取消第 2 页 octocat/gone 的星标。
	env.mu.Lock()
	env.starred[2] = []map[string]any{{"full_name": "octocat/keep2", "fork": false, "archived": false}}
	env.starredPages = nil
	env.mu.Unlock()
	env.poller.lastStarSync = time.Time{}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	gone, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/gone")
	if err != nil || gone.State != store.TrackerStateDisabled {
		t.Fatalf("多页列表末页场景下 unstar 的仓应被停用: %+v %v", gone, err)
	}
	for _, repo := range []string{"octocat/Hello-World", "octocat/keep2", "octocat/Spoon-Knife"} {
		tk, err := env.data.StarredTrackers().GetByFullName(ctx, repo)
		if err != nil || tk.State != store.TrackerStateTracking {
			t.Fatalf("仍在 star 列表的仓应保持追踪: %s %+v %v", repo, tk, err)
		}
	}
	// 翻页必须在末页（第 3 页）终止：不应请求越界空页。
	env.mu.Lock()
	pages := append([]int(nil), env.starredPages...)
	env.mu.Unlock()
	if len(pages) != 3 || pages[0] != 1 || pages[2] != 3 {
		t.Fatalf("3 页 star 列表应在末页终止（请求 1,2,3），got %v", pages)
	}
}

// TestStarredSyncStars_异常分页上限保护 验证越界空页持续下发 rel="next" 的异常上游
// 不会无限翻页：命中页码防御上限后中止并跳过本轮 unstar 移除（不误删仍在列表的仓）。
func TestStarredSyncStars_异常分页上限保护(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starNextForever = true
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	env.starredPages = nil
	env.mu.Unlock()
	if err := env.poller.SyncStarsNow(ctx); err != nil {
		t.Fatal(err)
	}
	env.mu.Lock()
	pages := append([]int(nil), env.starredPages...)
	env.mu.Unlock()
	// 防御上限：max/100+2（默认 500 → page > 7 中止），请求页码不得无界增长。
	if len(pages) == 0 || len(pages) > 8 {
		t.Fatalf("异常上游应在页码防御上限处中止，got %v", pages)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking {
		t.Fatalf("未完整拉全时不应停用追踪: %+v %v", tk, err)
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
	if got := store.PayloadString(events[0].PayloadSummary, "repository"); got != "octocat/Hello-World" {
		t.Fatalf("事件应带仓库名供摘要引用，got %q: %+v", got, events[0])
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

// TestStarredPollReleases_中断补拉全部新release 验证中断期间发布的多个 release
// 全部事件化（不再只处理最新一条导致中间版本静默丢失），游标推进到最新。
func TestStarredPollReleases_中断补拉全部新release(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 中断期间连发 3 个 release（最新在前）。
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{
		releaseMap(45, "v2.1", false), releaseMap(44, "v2.0", false), releaseMap(43, "v1.9", false),
	}
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	events := listReleaseEvents(t, env.data)
	if len(events) != 3 {
		t.Fatalf("中断期间 3 个 release 应全部事件化，got %d", len(events))
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.LastReleaseID != 45 {
		t.Fatalf("游标应推进到最新 release 45，got %d", tk.LastReleaseID)
	}
}

// TestStarredPollReleases_补发上限下轮续补 验证单轮补发达到上限后游标不动，
// 下轮节拍从旧游标继续补发剩余 release（重复事件幂等跳过、不消耗预算），不丢事件。
func TestStarredPollReleases_补发上限下轮续补(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 中断期间连发 7 个 release（id 43..49，最新在前）。
	var batch []map[string]any
	for id := int64(49); id >= 43; id-- {
		batch = append(batch, releaseMap(id, fmt.Sprintf("v1.%d", id-42), false))
	}
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = batch
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	if events := listReleaseEvents(t, env.data); len(events) != maxBackfillPerRound {
		t.Fatalf("首轮应补发 %d 条，got %d", maxBackfillPerRound, len(events))
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.LastReleaseID != 42 {
		t.Fatalf("达到上限后游标不应推进，got %d", tk.LastReleaseID)
	}
	// 下轮从旧游标续补剩余 release；已建事件幂等跳过，不重复消耗预算。
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	events := listReleaseEvents(t, env.data)
	if len(events) != 7 {
		t.Fatalf("两轮应补全 7 条事件，got %d", len(events))
	}
	tk, _ = env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.LastReleaseID != 49 {
		t.Fatalf("补全后游标应推进到最新 49，got %d", tk.LastReleaseID)
	}
}

// TestStarredPollReleases_翻页补拉超过一页 验证中断期间发布超过一页（>30 条）的 release
// 全部事件化：轮询分页翻到游标为止，不因单页上限丢失旧版本。
func TestStarredPollReleases_翻页补拉超过一页(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 中断期间连发 31 个 release（id 43..73，最新在前，超过一页 30 条）。
	var batch []map[string]any
	for id := int64(73); id >= 43; id-- {
		batch = append(batch, releaseMap(id, fmt.Sprintf("v1.%d", id-42), false))
	}
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = batch
	env.mu.Unlock()
	// 逐轮补发直到全部事件化（每轮 5 条），最后一轮需翻页到第 2 页。
	total := 0
	for i := 0; i < 20; i++ {
		env.poller.lastReleasePoll = time.Time{}
		if err := env.poller.PollReleases(ctx); err != nil {
			t.Fatal(err)
		}
		total = len(listReleaseEvents(t, env.data))
		if total == 31 {
			break
		}
	}
	if total != 31 {
		t.Fatalf("超过一页的 release 应全部事件化，got %d", total)
	}
	tk, _ := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if tk.LastReleaseID != 73 {
		t.Fatalf("补全后游标应推进到最新 73，got %d", tk.LastReleaseID)
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

// TestStarredPollReleases_304不误判无release 验证条件请求命中 304（release 未变化）时保持 tracking：
// 304 响应体为空是正常表示，不能被当成「无 release」转入 inactive（回归：空列表判定早于 304 处理）。
func TestStarredPollReleases_304不误判无release(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking || tk.ETag == "" {
		t.Fatalf("基线应 tracking 且带 etag: %+v %v", tk, err)
	}
	// release 未变化：下一轮条件请求命中 304（空 items + modified=false）。
	env.mu.Lock()
	env.releaseUnchanged["octocat/Hello-World"] = true
	env.mu.Unlock()
	env.poller.lastReleasePoll = time.Time{}
	if err := env.poller.PollReleases(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err = env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking {
		t.Fatalf("304 不应把有 release 的仓误标无 release: %+v %v", tk, err)
	}
	if tk.LastReleaseID != 42 || tk.LastReleaseTag != "v1.0" {
		t.Fatalf("304 不应改写游标: %+v", tk)
	}
}

// TestStarredSyncStars_带游标inactive自愈 验证 inactive 但带 release 游标的仓（304 误判受害）
// 在下次 star 同步时立即重新探测恢复 tracking；release 确实被删除的仓保持 inactive。
func TestStarredSyncStars_带游标inactive自愈(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	// 复现 304 误判：有 release 的仓被标为无 release（inactive + 复查时间）。
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking || tk.LastReleaseID == 0 {
		t.Fatalf("基线应 tracking 且带游标: %+v %v", tk, err)
	}
	if err := env.data.StarredTrackers().UpdateNoRelease(ctx, tk.ID, time.Now().UTC().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	env.poller.lastStarSync = time.Time{}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err = env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateTracking {
		t.Fatalf("带游标的 inactive 应在同步时自愈回 tracking: %+v %v", tk, err)
	}
	// release 被整体删除（列表真的为空）：重新探测后仍应保持 inactive。
	env.mu.Lock()
	env.releases["octocat/Hello-World"] = []map[string]any{}
	env.mu.Unlock()
	if err := env.data.StarredTrackers().UpdateNoRelease(ctx, tk.ID, time.Now().UTC().Add(7*24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	env.poller.lastStarSync = time.Time{}
	if err := env.poller.SyncStars(ctx); err != nil {
		t.Fatal(err)
	}
	tk, err = env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || tk.State != store.TrackerStateInactive {
		t.Fatalf("release 确实为空时不应恢复: %+v %v", tk, err)
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

// failListAllTrackers 包装真实 trackerStore，仅让 ListAll 注入失败。
type failListAllTrackers struct {
	store.StarredTrackerStore
}

func (s failListAllTrackers) ListAll(context.Context, int) ([]store.StarredRepoTracker, error) {
	return nil, fmt.Errorf("injected list failure")
}

// failTrackersStore 仅替换 StarredTrackers 访问器，其余存储方法沿用真实实现。
type failTrackersStore struct {
	store.Store
}

func (s failTrackersStore) StarredTrackers() store.StarredTrackerStore {
	return failListAllTrackers{s.Store.StarredTrackers()}
}

// TestStarredSyncStars_映射加载失败中止 守护：预加载 full_name→tracker 映射失败时
// 整轮中止（下节拍重试），存量 tracker 的状态与 release 游标不被误判为新仓而重置。
func TestStarredSyncStars_映射加载失败中止(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{
			{"full_name": "octocat/Hello-World", "fork": false, "archived": false},
		}
		env.releases["octocat/Hello-World"] = []map[string]any{releaseMap(42, "v1.0", false)}
	})
	ctx := t.Context()
	now := time.Now().UTC()
	// 预置存量 tracker：用户手动停用（disabled）且带 release 游标——若被误判为新仓，
	// registerIfNew 的 Upsert 会强制写回 tracking 并清零游标。
	if err := env.data.StarredTrackers().Upsert(ctx, store.StarredRepoTracker{
		ID: ulid.Make().String(), FullName: "octocat/Hello-World", State: store.TrackerStateDisabled,
		FirstSeenAt: now, LastReleaseID: 42, ETag: "e1", CreatedAt: now, UpdatedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	env.poller.Store = failTrackersStore{Store: env.data}
	// 中止以错误形式上报（调度器与手动同步可见本轮失败），与 ListUserStarred 失败同类。
	if err := env.poller.SyncStarsNow(ctx); err == nil {
		t.Fatal("映射加载失败应返回错误")
	}
	tk, err := env.data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil {
		t.Fatal(err)
	}
	if tk.State != store.TrackerStateDisabled || tk.LastReleaseID != 42 || tk.ETag != "e1" {
		t.Fatalf("映射加载失败中止不应改写存量追踪状态: %+v", tk)
	}
}

// TestStarredSyncStars_非法用户名跳过并留痕 验证写入侧漏校验的历史脏值不会每轮
// 拼出错误 API 路径空转：轮询端直接跳过本轮、推进记账，并 Warn 指明根因。
func TestStarredSyncStars_非法用户名跳过并留痕(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {
		env.starred[1] = []map[string]any{{"full_name": "octocat/Hello-World", "fork": false, "archived": false}}
	})
	var buf bytes.Buffer
	env.poller.Logger = slog.New(slog.NewJSONHandler(&buf, nil))
	if _, err := env.data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: SettingStarredUsername, ValueJSON: json.RawMessage(`"octo/cat"`),
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
	if err := env.poller.SyncStarsNow(t.Context()); err != nil {
		t.Fatal(err)
	}
	if _, err := env.data.StarredTrackers().GetByFullName(t.Context(), "octocat/Hello-World"); err != store.ErrNotFound {
		t.Fatalf("非法用户名不应注册任何追踪: %v", err)
	}
	if env.poller.LastStarSyncAt().IsZero() {
		t.Fatal("确定性跳过后应推进记账，避免每节拍空转")
	}
	if !strings.Contains(buf.String(), "star_sync_invalid_username") {
		t.Fatalf("应 Warn 留痕指明非法用户名: %s", buf.String())
	}
}

func TestStarredCreateReleaseEventSanitization(t *testing.T) {
	env := newStarredTestEnv(t, func(env *starredTestEnv) {})
	ctx := t.Context()
	now := time.Now().UTC()

	// 1. 测试首尾空格清理与 tag_name 回退
	created, err := env.poller.createReleaseEvent(ctx, "octocat/Hello-World", githubx.ReleaseItem{
		ID:          1001,
		TagName:     "  v1.0.0  ",
		Name:        "   ",
		Body:        "release body",
		PublishedAt: now,
		HTMLURL:     " https://github.com/octocat/Hello-World/releases/tag/v1.0.0 ",
		Author: struct {
			Login string `json:"login"`
		}{Login: " octocat "},
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created {
		t.Fatal("expected event to be created")
	}

	events := listReleaseEvents(t, env.data)
	if len(events) != 1 {
		t.Fatalf("expected 1 event, got %d", len(events))
	}
	ev := events[0]
	if ev.Title != "v1.0.0" {
		t.Fatalf("expected Title to fall back to trimmed TagName 'v1.0.0', got %q", ev.Title)
	}
	if ev.Actor != "octocat" {
		t.Fatalf("expected trimmed Actor 'octocat', got %q", ev.Actor)
	}
	if ev.HTMLURL != "https://github.com/octocat/Hello-World/releases/tag/v1.0.0" {
		t.Fatalf("expected trimmed HTMLURL, got %q", ev.HTMLURL)
	}
	if ev.PayloadSummary["tag_name"] != "v1.0.0" {
		t.Fatalf("expected trimmed payload tag_name, got %v", ev.PayloadSummary["tag_name"])
	}

	// 2. 测试 Name 和 TagName 均为空时的保底 Title
	created2, err := env.poller.createReleaseEvent(ctx, "octocat/Hello-World", githubx.ReleaseItem{
		ID:          1002,
		TagName:     "  ",
		Name:        "  ",
		PublishedAt: now.Add(time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	if !created2 {
		t.Fatal("expected second event to be created")
	}
	events2 := listReleaseEvents(t, env.data)
	var ev2 store.Event
	for _, e := range events2 {
		if e.SubjectNumber != nil && *e.SubjectNumber == 1002 {
			ev2 = e
			break
		}
	}
	if ev2.Title != "Release 1002" {
		t.Fatalf("expected default Title 'Release 1002', got %q", ev2.Title)
	}
}

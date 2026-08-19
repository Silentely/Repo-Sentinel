package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestActivateRepositoryWritesBaselineFinishedAt 验证基线仓库激活后状态与
// 基线结束时间一次写回（Upsert 单次更新，非 UpdateSyncStatus + Get + Upsert 三步）。
func TestActivateRepositoryWritesBaselineFinishedAt(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()
	now := time.Now().UTC()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-activate-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusBaseline,
		Owner: "acme", Name: "app", FullName: "acme/app", BaselineStartedAt: &now,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	// 未登录应拒绝。
	unauth := fixture.request(t, http.MethodPost, "/api/v1/repositories/"+repo.ID+"/activate",
		"", "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	ok := fixture.request(
		t, http.MethodPost, "/api/v1/repositories/"+repo.ID+"/activate",
		"", "127.0.0.1:45002", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if ok.Code != http.StatusOK {
		t.Fatalf("activate status=%d body=%s", ok.Code, ok.Body.String())
	}
	var activated store.Repository
	if err := json.Unmarshal(ok.Body.Bytes(), &activated); err != nil {
		t.Fatal(err)
	}
	if activated.SyncStatus != store.SyncStatusActive {
		t.Fatalf("激活后 sync_status=%q, want active", activated.SyncStatus)
	}
	if activated.BaselineFinishedAt == nil {
		t.Fatal("激活后应写 BaselineFinishedAt")
	}

	// 持久化核对：回读确认状态与时间均已落库（Upsert 覆盖 SyncStatus 字段）。
	stored, err := fixture.store.Repositories().Get(ctx, repo.ID)
	if err != nil {
		t.Fatalf("get repo: %v", err)
	}
	if stored.SyncStatus != store.SyncStatusActive {
		t.Fatalf("落库 sync_status=%q, want active", stored.SyncStatus)
	}
	if stored.BaselineFinishedAt == nil {
		t.Fatal("落库应含 BaselineFinishedAt")
	}
}

func Test工作项忽略API需要认证与CSRF并支持列表筛选(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()
	now := time.Now().UTC()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-http-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "app", FullName: "acme/app",
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	item, _, err := fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID: "wi-http-1", RepositoryID: repo.ID, Number: 7, Kind: store.WorkItemKindIssue,
		State: "open", Title: "long-lived issue", SourceUpdatedAt: now, StateHash: "h1",
	})
	if err != nil {
		t.Fatalf("upsert work item: %v", err)
	}

	// 未登录应拒绝。
	unauth := fixture.request(t, http.MethodPatch, "/api/v1/work-items/"+item.ID+"/ignored",
		`{"ignored":true}`, "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 缺 CSRF 应拒绝。
	noCSRF := fixture.request(t, http.MethodPatch, "/api/v1/work-items/"+item.ID+"/ignored",
		`{"ignored":true}`, "127.0.0.1:45002", cookies, nil)
	assertAPIError(t, noCSRF, http.StatusForbidden, "csrf_failed")

	// 正常忽略。
	ok := fixture.request(
		t, http.MethodPatch, "/api/v1/work-items/"+item.ID+"/ignored",
		`{"ignored":true}`, "127.0.0.1:45003", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if ok.Code != http.StatusOK {
		t.Fatalf("ignore status=%d body=%s", ok.Code, ok.Body.String())
	}
	var ignored store.WorkItem
	if err := json.Unmarshal(ok.Body.Bytes(), &ignored); err != nil {
		t.Fatal(err)
	}
	if !ignored.Ignored {
		t.Fatalf("期望 ignored=true，got %+v", ignored)
	}

	// 默认列表不含已忽略项。
	listActive := fixture.request(t, http.MethodGet, "/api/v1/work-items?kind=issue&state=open&per_page=20",
		"", "127.0.0.1:45004", cookies, nil)
	if listActive.Code != http.StatusOK {
		t.Fatalf("list active status=%d body=%s", listActive.Code, listActive.Body.String())
	}
	var activePage struct {
		Items []store.WorkItem `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(listActive.Body.Bytes(), &activePage); err != nil {
		t.Fatal(err)
	}
	if activePage.Total != 0 {
		t.Fatalf("默认列表应排除已忽略，got total=%d", activePage.Total)
	}

	// ignored=true 仅返回已忽略。
	listIgnored := fixture.request(t, http.MethodGet, "/api/v1/work-items?kind=issue&ignored=true&per_page=20",
		"", "127.0.0.1:45005", cookies, nil)
	if listIgnored.Code != http.StatusOK {
		t.Fatalf("list ignored status=%d body=%s", listIgnored.Code, listIgnored.Body.String())
	}
	var ignoredPage struct {
		Items []store.WorkItem `json:"items"`
		Total int              `json:"total"`
	}
	if err := json.Unmarshal(listIgnored.Body.Bytes(), &ignoredPage); err != nil {
		t.Fatal(err)
	}
	if ignoredPage.Total != 1 || len(ignoredPage.Items) != 1 || ignoredPage.Items[0].ID != item.ID {
		t.Fatalf("ignored 列表应含 wi-http-1，got %+v", ignoredPage)
	}

	// 取消忽略。
	restore := fixture.request(
		t, http.MethodPatch, "/api/v1/work-items/"+item.ID+"/ignored",
		`{"ignored":false}`, "127.0.0.1:45006", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if restore.Code != http.StatusOK {
		t.Fatalf("unignore status=%d body=%s", restore.Code, restore.Body.String())
	}

	// 不存在的 ID。
	missing := fixture.request(
		t, http.MethodPatch, "/api/v1/work-items/does-not-exist/ignored",
		`{"ignored":true}`, "127.0.0.1:45007", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if missing.Code == http.StatusOK {
		t.Fatalf("不存在的资源不应成功: %s", missing.Body.String())
	}
}

func Test系统设置保留天数校验与回读(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 默认值应包含保留天数。
	get := fixture.request(t, http.MethodGet, "/api/v1/system/settings", "", "127.0.0.1:45201", cookies, nil)
	if get.Code != http.StatusOK {
		t.Fatalf("get settings status=%d body=%s", get.Code, get.Body.String())
	}
	var defaults map[string]any
	if err := json.Unmarshal(get.Body.Bytes(), &defaults); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"retention.events_days":             90,
		"retention.outbox_days":             30,
		"retention.webhook_deliveries_days": 30,
	} {
		if got, ok := defaults[key].(float64); !ok || got != want {
			t.Fatalf("默认 %s=%v，期望 %v；完整响应=%v", key, defaults[key], want, defaults)
		}
	}

	// 非法值应拒绝且返回 validation_failed。
	for _, body := range []string{
		`{"retention.events_days":-1}`,
		`{"retention.outbox_days":3651}`,
		`{"retention.events_days":1.5}`,
		`{"retention.webhook_deliveries_days":"90"}`,
	} {
		resp := fixture.request(t, http.MethodPut, "/api/v1/system/settings", body,
			"127.0.0.1:45202", cookies, map[string]string{CSRFHeaderName: csrf.Value})
		assertAPIError(t, resp, http.StatusBadRequest, "validation_failed")
	}

	// 合法值（含 0=禁用清理）应写入并回读。
	put := fixture.request(t, http.MethodPut, "/api/v1/system/settings",
		`{"retention.events_days":0,"retention.outbox_days":45,"retention.webhook_deliveries_days":7}`,
		"127.0.0.1:45203", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if put.Code != http.StatusOK {
		t.Fatalf("put settings status=%d body=%s", put.Code, put.Body.String())
	}
	var updated map[string]any
	if err := json.Unmarshal(put.Body.Bytes(), &updated); err != nil {
		t.Fatal(err)
	}
	for key, want := range map[string]float64{
		"retention.events_days":             0,
		"retention.outbox_days":             45,
		"retention.webhook_deliveries_days": 7,
	} {
		if got, ok := updated[key].(float64); !ok || got != want {
			t.Fatalf("更新后 %s=%v，期望 %v；完整响应=%v", key, updated[key], want, updated)
		}
	}

	// 未认证与缺 CSRF 仍应拒绝。
	unauth := fixture.request(t, http.MethodPut, "/api/v1/system/settings",
		`{"retention.events_days":30}`, "127.0.0.1:45204", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")
	noCSRF := fixture.request(t, http.MethodPut, "/api/v1/system/settings",
		`{"retention.events_days":30}`, "127.0.0.1:45205", cookies, nil)
	assertAPIError(t, noCSRF, http.StatusForbidden, "csrf_failed")
}

func TestDashboard统计排除归档仓与已忽略项(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	ctx := t.Context()
	now := time.Now().UTC()

	active, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-dash-a", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "a", FullName: "o/a",
	})
	if err != nil {
		t.Fatal(err)
	}
	archived, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-dash-b", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusArchived,
		Owner: "o", Name: "b", FullName: "o/b", IsArchived: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, item := range []store.WorkItem{
		{ID: "di-1", RepositoryID: active.ID, Number: 1, Kind: store.WorkItemKindIssue, State: "open", Title: "i1", SourceUpdatedAt: now, StateHash: "1"},
		{ID: "di-2", RepositoryID: active.ID, Number: 2, Kind: store.WorkItemKindIssue, State: "open", Title: "i2", SourceUpdatedAt: now, StateHash: "2"},
		{ID: "di-3", RepositoryID: archived.ID, Number: 3, Kind: store.WorkItemKindIssue, State: "open", Title: "i3", SourceUpdatedAt: now, StateHash: "3"},
		{ID: "dp-1", RepositoryID: active.ID, Number: 4, Kind: store.WorkItemKindPR, State: "open", Title: "p1", SourceUpdatedAt: now, StateHash: "4"},
	} {
		if _, _, err := fixture.store.WorkItems().UpsertIfNewer(ctx, item); err != nil {
			t.Fatal(err)
		}
	}
	if err := fixture.store.WorkItems().SetIgnored(ctx, "di-2", true); err != nil {
		t.Fatal(err)
	}

	resp := fixture.request(t, http.MethodGet, "/api/v1/dashboard", "", "127.0.0.1:45101", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("dashboard status=%d body=%s", resp.Code, resp.Body.String())
	}
	var stats store.DashboardStats
	if err := json.Unmarshal(resp.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.OpenIssues != 1 {
		t.Fatalf("open_issues want 1 got %d", stats.OpenIssues)
	}
	if stats.OpenPulls != 1 {
		t.Fatalf("open_pulls want 1 got %d", stats.OpenPulls)
	}
}

func Test渠道订阅配置API(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	csrfHeader := map[string]string{CSRFHeaderName: csrf.Value}

	// 创建时携带订阅配置。
	created := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"Telegram","enabled":true,"target":"-1001","event_kinds":["issue","workflow_run"],"digest_enabled":false}`,
		"127.0.0.1:46001", cookies, csrfHeader)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	// 列表应回读新字段。
	list := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels",
		"", "127.0.0.1:46002", cookies, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("list status=%d", list.Code)
	}
	var page struct {
		Items []struct {
			ID            string   `json:"id"`
			EventKinds    []string `json:"event_kinds"`
			DigestEnabled bool     `json:"digest_enabled"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 {
		t.Fatalf("期望 1 条渠道，got %d", len(page.Items))
	}
	if len(page.Items[0].EventKinds) != 2 || page.Items[0].EventKinds[0] != "issue" {
		t.Fatalf("event_kinds 回读失败: %+v", page.Items[0].EventKinds)
	}
	if page.Items[0].DigestEnabled {
		t.Fatal("digest_enabled 应为 false")
	}

	// 省略新字段时应保留现值。
	updated := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"Telegram","enabled":true,"target":"-1001"}`,
		"127.0.0.1:46003", cookies, csrfHeader)
	if updated.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", updated.Code, updated.Body.String())
	}
	relists := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels",
		"", "127.0.0.1:46004", cookies, nil)
	var page2 struct {
		Items []struct {
			EventKinds    []string `json:"event_kinds"`
			DigestEnabled bool     `json:"digest_enabled"`
		} `json:"items"`
	}
	if err := json.Unmarshal(relists.Body.Bytes(), &page2); err != nil {
		t.Fatal(err)
	}
	if len(page2.Items) != 1 || len(page2.Items[0].EventKinds) != 2 || page2.Items[0].DigestEnabled {
		t.Fatalf("省略字段应保留现值，got kinds=%v digest=%v", page2.Items[0].EventKinds, page2.Items[0].DigestEnabled)
	}

	// 非法 kind 应 400。
	bad := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"Telegram","enabled":true,"target":"-1001","event_kinds":["issue","nope"]}`,
		"127.0.0.1:46005", cookies, csrfHeader)
	assertAPIError(t, bad, http.StatusBadRequest, errorCodeValidationFailed)

	// 新渠道省略订阅字段时：event_kinds 为 null（订阅全部）且每日汇总默认开。
	createdDefault := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"hook","enabled":true,"target":"https://example.com/hook"}`,
		"127.0.0.1:46006", cookies, csrfHeader)
	if createdDefault.Code != http.StatusOK {
		t.Fatalf("create default status=%d body=%s", createdDefault.Code, createdDefault.Body.String())
	}
	listDefault := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels",
		"", "127.0.0.1:46007", cookies, nil)
	var page3 struct {
		Items []struct {
			ChannelType   string   `json:"channel_type"`
			EventKinds    []string `json:"event_kinds"`
			DigestEnabled bool     `json:"digest_enabled"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listDefault.Body.Bytes(), &page3); err != nil {
		t.Fatal(err)
	}
	if len(page3.Items) != 2 {
		t.Fatalf("期望 2 条渠道，got %d", len(page3.Items))
	}
	foundHook := false
	var hookKinds []string
	hookDigest := false
	for _, item := range page3.Items {
		if item.ChannelType == "http_webhook" {
			foundHook, hookKinds, hookDigest = true, item.EventKinds, item.DigestEnabled
		}
	}
	if !foundHook {
		t.Fatal("缺少 http_webhook 渠道")
	}
	if hookKinds != nil {
		t.Fatalf("省略 event_kinds 应保持 null=订阅全部: %v", hookKinds)
	}
	if !hookDigest {
		t.Fatal("省略 digest_enabled 应默认 true")
	}
}

// 设置批量写入必须先全量校验：任何一个键非法时整批拒绝，不得出现"部分落库"。
func TestSettingsPutValidatesAllBeforeWriting(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	bad := fixture.request(t, http.MethodPut, "/api/v1/system/settings",
		`{"digest.local_time":"25:99","retention.events_days":45}`, "127.0.0.1:45100", cookies, headers)
	assertAPIError(t, bad, http.StatusBadRequest, errorCodeValidationFailed)

	if _, err := fixture.store.Settings().Get(t.Context(), "retention.events_days"); err != store.ErrNotFound {
		t.Fatalf("校验失败的批量写入不应部分落库，err=%v", err)
	}

	ok := fixture.request(t, http.MethodPut, "/api/v1/system/settings",
		`{"digest.local_time":"9:5","retention.events_days":45,"notify.burst_threshold":30,"feature.issues":false}`,
		"127.0.0.1:45101", cookies, headers)
	if ok.Code != http.StatusOK {
		t.Fatalf("PUT settings status=%d body=%s", ok.Code, ok.Body.String())
	}
	var out map[string]any
	if err := json.Unmarshal(ok.Body.Bytes(), &out); err != nil {
		t.Fatal(err)
	}
	if out["digest.local_time"] != "09:05" {
		t.Fatalf("local_time 应归一化为 09:05，got %v", out["digest.local_time"])
	}
	if out["retention.events_days"] != float64(45) {
		t.Fatalf("retention 应为 45，got %v", out["retention.events_days"])
	}
	// GET 必须返回 burst_window_sec（此前 PUT 白名单接受但 GET 不返回）。
	if out["notify.burst_window_sec"] != float64(300) {
		t.Fatalf("GET 应返回 burst_window_sec 默认 300，got %v", out["notify.burst_window_sec"])
	}
	// 新增 report.* 定期报告键必须有默认值返回，且不随 PUT 丢失。
	for key, want := range map[string]any{
		"report.weekly_enabled":  false,
		"report.weekly_day":      "monday",
		"report.monthly_enabled": false,
		"report.monthly_day":     float64(1),
	} {
		if out[key] != want {
			t.Fatalf("GET 应返回 %s 默认 %v，got %v", key, want, out[key])
		}
	}
}

func TestValidateSettingValueRules(t *testing.T) {
	cases := []struct {
		key   string
		value any
		ok    bool
		want  any
	}{
		{"digest.local_time", "23:59", true, "23:59"},
		{"digest.local_time", "9:05", true, "09:05"},
		{"digest.local_time", "25:99", false, nil},
		{"digest.local_time", "12:30junk", false, nil},
		{"digest.local_time", 123, false, nil},
		{"admin.timezone", "Asia/Shanghai", true, "Asia/Shanghai"},
		{"admin.timezone", "Mars/Olympus", false, nil},
		{"notify.aggregate_window_sec", float64(120), true, 120},
		{"notify.aggregate_window_sec", float64(0), false, nil},
		{"notify.aggregate_window_sec", float64(1.5), false, nil},
		{"notify.burst_threshold", float64(10001), false, nil},
		{"display.closed_limit", float64(0), false, nil},
		{"feature.issues", true, true, true},
		{"feature.issues", "yes", false, nil},
		{"retention.events_days", float64(0), true, 0},
		{"retention.events_days", float64(3651), false, nil},
		{"report.weekly_enabled", true, true, true},
		{"report.weekly_enabled", "yes", false, nil},
		{"report.weekly_day", "Friday", true, "friday"},
		{"report.weekly_day", "funday", false, nil},
		{"report.weekly_day", 3, false, nil},
		{"report.monthly_enabled", true, true, true},
		{"report.monthly_day", float64(28), true, 28},
		{"report.monthly_day", float64(29), false, nil},
		{"report.monthly_day", float64(0), false, nil},
	}
	for _, tc := range cases {
		got, _, ok := validateSettingValue(tc.key, tc.value)
		if ok != tc.ok {
			t.Fatalf("%s=%v 校验结果 want %v got %v", tc.key, tc.value, tc.ok, ok)
		}
		if ok && got != tc.want {
			t.Fatalf("%s=%v 归一化 want %v got %v", tc.key, tc.value, tc.want, got)
		}
	}
}

// toggleableChannelFailure 包装真实 Store：broken 置位后仅 Channels() 返回
// GetEnabledByType 必失败的实现，其余子存储原样透传（登录态与 CSRF 依赖它们保持可用）。
// 经 httpTestOptions.decorateStore 注入 handler 装配，认证/会话路径不受影响。
type toggleableChannelFailure struct {
	store.Store
	broken *atomic.Bool
	err    error
}

func (s *toggleableChannelFailure) Channels() store.ChannelStore {
	if s.broken.Load() {
		return failingChannelStore{ChannelStore: s.Store.Channels(), err: s.err}
	}
	return s.Store.Channels()
}

// failingChannelStore 仅让 GetEnabledByType 失败，其余方法透传真实实现。
type failingChannelStore struct {
	store.ChannelStore
	err error
}

func (f failingChannelStore) GetEnabledByType(context.Context, string) (store.NotificationChannel, error) {
	return store.NotificationChannel{}, f.err
}

func Test渠道Upsert在渠道存储故障时返回500而非静默创建(t *testing.T) {
	// 保护行为：handleUpsertChannel 中 GetEnabledByType 返回 DB 故障（非 ErrNotFound）时，
	// 必须向上返回 500 internal_error；历史 bug 把故障当作"无既有渠道"继续静默创建，
	// 会生成重复渠道并丢失原配置。这里只让渠道子存储按开关失败，认证与 CSRF 不受影响，
	// 因此 500 必然来自 handler 的错误分支本身。
	broken := &atomic.Bool{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		decorateStore: func(inner store.Store) store.Store {
			return &toggleableChannelFailure{
				Store:  inner,
				broken: broken,
				err:    errors.New("fixture: 注入的渠道存储故障"),
			}
		},
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	body := `{"name":"Telegram","enabled":true,"target":"-1001"}`
	created := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		body, "127.0.0.1:46301", cookies, headers)
	if created.Code != http.StatusOK {
		t.Fatalf("首次创建 status=%d body=%s", created.Code, created.Body.String())
	}

	broken.Store(true)
	failed := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		body, "127.0.0.1:46302", cookies, headers)
	assertAPIError(t, failed, http.StatusInternalServerError, errorCodeInternal)

	// 恢复存储后回读：失败请求不得静默创建出第二条同类型渠道。
	broken.Store(false)
	list := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels",
		"", "127.0.0.1:46303", cookies, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("回读渠道列表 status=%d body=%s", list.Code, list.Body.String())
	}
	var page struct {
		Items []struct {
			ChannelType string `json:"channel_type"`
		} `json:"items"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &page); err != nil {
		t.Fatal(err)
	}
	telegramCount := 0
	for _, item := range page.Items {
		if item.ChannelType == "telegram" {
			telegramCount++
		}
	}
	if telegramCount != 1 {
		t.Fatalf("存储故障期间不应静默创建渠道，telegram 渠道数=%d，期望 1", telegramCount)
	}
}

func Test渠道Upsert在底层存储关闭后绝不静默成功(t *testing.T) {
	// 保护行为（行为契约级）：底层存储整体不可用（连接已关闭）时，渠道写接口无论故障
	// 暴露在认证、会话还是渠道读写路径，都必须返回内部错误而非 2xx 静默成功——
	// 与上一个测试互补：那个测试精确锁定 handler 分支，这个测试覆盖端到端契约。
	// 注意：database/sql 的 Close 幂等，fixture 清理阶段再次 Close 不会报错。
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	body := `{"name":"hook","enabled":true,"target":"https://example.com/hook"}`
	created := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		body, "127.0.0.1:46311", cookies, headers)
	if created.Code != http.StatusOK {
		t.Fatalf("首次创建 status=%d body=%s", created.Code, created.Body.String())
	}

	if err := fixture.store.Close(); err != nil {
		t.Fatalf("关闭测试存储失败: %v", err)
	}
	afterClose := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		body, "127.0.0.1:46312", cookies, headers)
	if afterClose.Code >= 200 && afterClose.Code < 300 {
		t.Fatalf("存储已关闭仍返回 2xx（疑似静默成功）: %s", afterClose.Body.String())
	}
	assertAPIError(t, afterClose, http.StatusInternalServerError, errorCodeInternal)
}

func TestOutbox空渠道匹配置退分支归一化分页参数(t *testing.T) {
	// 保护行为：按 channel_type 过滤且渠道表无任何匹配时，handleListOutbox 的置退响应
	// 必须与其他列表端点一致地归一化分页参数（page=1、per_page=20），items 为 [],
	// 而非回显请求中的 0/0（历史 bug 直接把未归一化的 f.Page/f.PerPage 写进响应）。
	// 路由以 server.go 为准：GET /api/v1/notifications/outbox。
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	const path = "/api/v1/notifications/outbox?channel_type=does-not-exist&page=0&per_page=0"
	unauth := fixture.request(t, http.MethodGet, path, "", "127.0.0.1:46321", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	resp := fixture.request(t, http.MethodGet, path, "", "127.0.0.1:46322", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("outbox 列表 status=%d body=%s", resp.Code, resp.Body.String())
	}
	var page struct {
		Items   []any `json:"items"`
		Page    int   `json:"page"`
		PerPage int   `json:"per_page"`
		Total   int   `json:"total"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &page); err != nil {
		t.Fatalf("outbox 响应不是合法 JSON: %v", err)
	}
	if page.Page != 1 || page.PerPage != 20 || page.Total != 0 {
		t.Fatalf("置退分页=(page=%d,per_page=%d,total=%d)，期望 (1,20,0)", page.Page, page.PerPage, page.Total)
	}
	if page.Items == nil {
		t.Fatal("空匹配时 items 必须是空数组 []，不得为 null")
	}
}

func TestHandleStarTrend(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	ctx := t.Context()

	// 造一个活跃仓 + 两条快照（08-02 缺，聚合应向前补值）。
	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-trend-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "o", Name: "a", FullName: "o/a", StarsEnabled: true, WatchesEnabled: true,
	}); err != nil {
		t.Fatal(err)
	}
	for _, in := range []store.RepoStatSnapshot{
		{RepositoryID: "repo-trend-1", Metric: store.MetricStargazers, Value: 10, SampleDate: "2026-08-01"},
		{RepositoryID: "repo-trend-1", Metric: store.MetricStargazers, Value: 15, SampleDate: "2026-08-03"},
	} {
		if _, err := fixture.store.RepoStatSnapshots().Upsert(ctx, in); err != nil {
			t.Fatal(err)
		}
	}

	// 未登录应拒绝。
	unauth := fixture.request(t, http.MethodGet, "/api/v1/stats/star-trend", "", "127.0.0.1:45401", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// days=0 全量：首点为最早快照日 08-01，total=10。
	resp := fixture.request(t, http.MethodGet, "/api/v1/stats/star-trend?days=0", "", "127.0.0.1:45402", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", resp.Code, resp.Body.String())
	}
	var body struct {
		Items []store.StarTrendPoint `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if len(body.Items) == 0 || body.Items[0].Date != "2026-08-01" || body.Items[0].Total != 10 {
		t.Fatalf("unexpected trend: %+v", body.Items)
	}

	// 非法 days 回退 30，仍应 200。
	bad := fixture.request(t, http.MethodGet, "/api/v1/stats/star-trend?days=abc", "", "127.0.0.1:45403", cookies, nil)
	if bad.Code != http.StatusOK {
		t.Fatalf("invalid days status = %d, body = %s", bad.Code, bad.Body.String())
	}
}

// TestDeleteRepositoryCascadeViaAPI 手动彻底删除：DELETE /api/v1/repositories/{id}
// 需认证 + CSRF，成功后仓库与关联数据（PR/事件）全部消失，重复删除返回 not_found。
func TestDeleteRepositoryCascadeViaAPI(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()
	now := time.Now().UTC()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-del-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusUnavailable,
		Owner: "acme", Name: "ternssh", FullName: "acme/ternssh",
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	if _, _, err := fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID: "wi-del-1", RepositoryID: repo.ID, Number: 1, Kind: store.WorkItemKindPR,
		State: "open", Title: "stale pr", SourceUpdatedAt: now, StateHash: "h1",
	}); err != nil {
		t.Fatalf("upsert work item: %v", err)
	}
	repoID := repo.ID
	if _, err := fixture.store.Events().Create(ctx, store.Event{
		ID: "ev-del-1", Source: "webhook", Kind: store.WorkItemKindPR, Action: "opened",
		RepositoryID: &repoID, Title: "stale pr", OccurredAt: now, DedupeFingerprint: "fp-del-1",
	}); err != nil {
		t.Fatalf("create event: %v", err)
	}

	// 未登录拒绝。
	unauth := fixture.request(t, http.MethodDelete, "/api/v1/repositories/"+repo.ID,
		"", "127.0.0.1:45101", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 缺 CSRF 拒绝。
	noCSRF := fixture.request(t, http.MethodDelete, "/api/v1/repositories/"+repo.ID,
		"", "127.0.0.1:45102", cookies, nil)
	assertAPIError(t, noCSRF, http.StatusForbidden, "csrf_failed")

	// 正常删除。
	ok := fixture.request(t, http.MethodDelete, "/api/v1/repositories/"+repo.ID,
		"", "127.0.0.1:45103", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if ok.Code != http.StatusOK {
		t.Fatalf("delete status=%d body=%s", ok.Code, ok.Body.String())
	}
	if _, err := fixture.store.Repositories().Get(ctx, repo.ID); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("仓库应已删除，got err=%v", err)
	}
	if _, err := fixture.store.WorkItems().GetByRepoNumber(ctx, repo.ID, 1); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("关联 PR 应已删除，got err=%v", err)
	}
	if _, err := fixture.store.Events().GetByFingerprint(ctx, "fp-del-1"); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("关联事件应已删除，got err=%v", err)
	}

	// 重复删除返回 not_found。
	again := fixture.request(t, http.MethodDelete, "/api/v1/repositories/"+repo.ID,
		"", "127.0.0.1:45104", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, again, http.StatusNotFound, "not_found")
}

// TestTestChannelCreatesDatedOutbox 测试通知必须写入 outbox 且正文带发送时刻：
// 多条测试通知内容相同会让人无法确认收到的是哪一条。
func TestTestChannelCreatesDatedOutbox(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	csrfHeader := map[string]string{CSRFHeaderName: csrf.Value}

	created := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"Telegram","enabled":true,"target":"-1001"}`,
		"127.0.0.1:45110", cookies, csrfHeader)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	resp := fixture.request(t, http.MethodPost, "/api/v1/notifications/channels/telegram/test",
		`{}`, "127.0.0.1:45111", cookies, csrfHeader)
	if resp.Code != http.StatusOK {
		t.Fatalf("test status=%d body=%s", resp.Code, resp.Body.String())
	}

	items, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{PerPage: 10})
	if err != nil {
		t.Fatal(err)
	}
	if len(items) != 1 {
		t.Fatalf("期望 1 条测试通知，got %d", len(items))
	}
	if items[0].Title != "🔔 测试通知" {
		t.Fatalf("标题不符: %q", items[0].Title)
	}
	if !strings.Contains(items[0].BodyText, "发送于 ") || !strings.Contains(items[0].BodyText, " UTC") {
		t.Fatalf("测试通知正文应带发送时刻（UTC），实际: %q", items[0].BodyText)
	}
}

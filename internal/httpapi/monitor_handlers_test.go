package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

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

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

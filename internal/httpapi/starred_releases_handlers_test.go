package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// TestStarredReleasesConfigAPI 覆盖配置读取、保存（含用户名归一化）与校验。
func TestStarredReleasesConfigAPI(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 未认证拒绝。
	unauthorized := fixture.request(t, http.MethodGet, "/api/v1/starred-releases/config", "", "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized")

	// 默认值视图。
	getOK := fixture.request(t, http.MethodGet, "/api/v1/starred-releases/config", "", "127.0.0.1:45002", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	var view starredReleasesConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Username != "" || view.MaxTrackers != 500 || !view.Enabled {
		t.Fatalf("默认视图不正确: %+v", view)
	}
	if view.StarSyncInterval == "" || view.ReleasePollInterval == "" {
		t.Fatalf("默认周期不应为空: %+v", view)
	}

	// 缺少 CSRF 的 PUT 拒绝。
	putMissingCSRF := fixture.request(t, http.MethodPut, "/api/v1/starred-releases/config",
		`{"username":"octocat"}`, "127.0.0.1:45003", cookies, nil)
	assertAPIError(t, putMissingCSRF, http.StatusForbidden, "csrf_failed")

	// 保存：用户名粘贴完整 URL 应归一化，周期/上限/预发布开关一并写入。
	putOK := fixture.request(t, http.MethodPut, "/api/v1/starred-releases/config",
		`{"username":"https://github.com/octocat/","star_sync_interval":"12h","release_poll_interval":"5m","max_trackers":200,"notify_prerelease":true,"enabled":false}`,
		"127.0.0.1:45004", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if putOK.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putOK.Code, putOK.Body.String())
	}
	if err := json.Unmarshal(putOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Username != "octocat" || view.MaxTrackers != 200 || !view.NotifyPrerelease || view.Enabled {
		t.Fatalf("PUT 后视图应反映保存值（用户名归一化、enabled=false）: %+v", view)
	}
	if view.StarSyncInterval != "12h0m0s" || view.ReleasePollInterval != "5m0s" {
		t.Fatalf("周期未生效: %+v", view)
	}

	// 非法周期拒绝。
	putBad := fixture.request(t, http.MethodPut, "/api/v1/starred-releases/config",
		`{"release_poll_interval":"25h"}`, "127.0.0.1:45005", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, putBad, http.StatusBadRequest, "validation_failed")
}

// TestStarredReleasesSync 覆盖立即同步端点。
func TestStarredReleasesSync(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 未配置用户名 → started:false。
	syncEmpty := fixture.request(t, http.MethodPost, "/api/v1/starred-releases/sync",
		"", "127.0.0.1:45006", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if syncEmpty.Code != http.StatusOK {
		t.Fatalf("POST status=%d body=%s", syncEmpty.Code, syncEmpty.Body.String())
	}
	var resp struct {
		Started bool `json:"started"`
	}
	if err := json.Unmarshal(syncEmpty.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if resp.Started {
		t.Fatal("无用户名时应 started=false")
	}
}

// TestStarredReleasesTrackers 覆盖追踪列表与单仓状态变更。
func TestStarredReleasesTrackers(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()

	now := time.Now().UTC()
	tk := store.StarredRepoTracker{
		ID: ulid.Make().String(), FullName: "octocat/Hello-World", State: store.TrackerStateTracking,
		LastReleaseTag: "v1.0", FirstSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := fixture.store.StarredTrackers().Upsert(ctx, tk); err != nil {
		t.Fatal(err)
	}

	// 列表（含分页与 state 筛选）。
	list := fixture.request(t, http.MethodGet, "/api/v1/starred-releases/trackers?page=1&per_page=10", "", "127.0.0.1:45007", cookies, nil)
	if list.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", list.Code, list.Body.String())
	}
	var listResp struct {
		Items []starredTrackerItem `json:"items"`
		Total int                  `json:"total"`
	}
	if err := json.Unmarshal(list.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if listResp.Total != 1 || len(listResp.Items) != 1 || listResp.Items[0].FullName != "octocat/Hello-World" {
		t.Fatalf("列表不正确: %+v", listResp)
	}
	// state 筛选不匹配 → 空。
	filtered := fixture.request(t, http.MethodGet, "/api/v1/starred-releases/trackers?state=disabled", "", "127.0.0.1:45008", cookies, nil)
	var filteredResp struct {
		Total int `json:"total"`
	}
	if err := json.Unmarshal(filtered.Body.Bytes(), &filteredResp); err != nil {
		t.Fatal(err)
	}
	if filteredResp.Total != 0 {
		t.Fatalf("disabled 筛选应无结果: %+v", filteredResp)
	}

	// 非法状态拒绝。
	badState := fixture.request(t, http.MethodPost, "/api/v1/starred-releases/trackers/"+tk.ID+"/state",
		`{"state":"bogus"}`, "127.0.0.1:45009", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, badState, http.StatusBadRequest, "validation_failed")

	// 停用成功。
	okState := fixture.request(t, http.MethodPost, "/api/v1/starred-releases/trackers/"+tk.ID+"/state",
		`{"state":"disabled"}`, "127.0.0.1:45010", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if okState.Code != http.StatusOK {
		t.Fatalf("POST state status=%d body=%s", okState.Code, okState.Body.String())
	}
	got, err := fixture.store.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || got.State != store.TrackerStateDisabled {
		t.Fatalf("状态应变为 disabled: %+v %v", got, err)
	}
}

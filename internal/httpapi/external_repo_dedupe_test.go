package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestAddExternalRepositoryDedupeBeforeLimit 回归：查重必须早于外部仓上限判定。
// 顺序反了（先判上限）时，列表已满的情况下重复提交一个已登记的外部仓会拿到
// 409 external_repo_limit，与幂等语义矛盾。
func TestAddExternalRepositoryDedupeBeforeLimit(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}
	ctx := t.Context()

	// 灌满外部仓上限，其中 repo-00 作为重复提交目标。
	for i := 0; i < store.MaxExternalRepositories; i++ {
		owner, name := "octo", fmt.Sprintf("repo-%02d", i)
		if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
			ID: fmt.Sprintf("repo-cap-%02d", i), Type: store.RepositoryTypeExternal,
			SyncStatus: store.SyncStatusActive, Owner: owner, Name: name,
			FullName: owner + "/" + name, HTMLURL: "https://github.com/" + owner + "/" + name,
		}); err != nil {
			t.Fatalf("seed external repo %d: %v", i, err)
		}
	}

	dup := fixture.request(t, http.MethodPost, "/api/v1/repositories/external",
		`{"full_name":"octo/repo-00"}`, "127.0.0.1:46101", cookies, headers)
	if dup.Code != http.StatusOK {
		t.Fatalf("上限已满时重复提交已登记仓库应幂等 200，实际 %d body=%s", dup.Code, dup.Body.String())
	}
	var dupBody struct {
		store.Repository
		AlreadyRegistered bool `json:"already_registered"`
	}
	if err := json.Unmarshal(dup.Body.Bytes(), &dupBody); err != nil {
		t.Fatalf("响应不是合法 JSON: %v", err)
	}
	if !dupBody.AlreadyRegistered {
		t.Fatalf("幂等分支应标记 already_registered，实际 body=%s", dup.Body.String())
	}
	if dupBody.ID != "repo-cap-00" || dupBody.SyncStatus != store.SyncStatusActive {
		t.Fatalf("应返回既有行且不重置同步状态，实际 id=%q status=%q", dupBody.ID, dupBody.SyncStatus)
	}

	// 上限内的新仓库仍被拒绝。
	fresh := fixture.request(t, http.MethodPost, "/api/v1/repositories/external",
		`{"full_name":"octo/brand-new"}`, "127.0.0.1:46102", cookies, headers)
	assertAPIError(t, fresh, http.StatusConflict, errorCodeExternalRepoLimit)
}

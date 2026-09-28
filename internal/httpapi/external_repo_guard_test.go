package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestAddExternalRepositoryRejectsExistingInstallationRepo 回归：「添加外部公开仓库」
// 对已作为安装仓存在的 full_name 必须拒绝。原实现只填 7 个字段且不查重，Upsert 更新路径
// 又无条件覆写 type/sync_status/is_archived/is_private，会把正在被安装令牌对账的私有仓
// 静默改判为 external_public、重置同步状态并抹掉私有标记，随后退出安装对账并被匿名轮询
// 404 钉成 unavailable 而永久跳过，接口却返回 201 不报错。
func TestAddExternalRepositoryRejectsExistingInstallationRepo(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()

	seeded, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-inst-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "payments", FullName: "acme/payments",
		HTMLURL: "https://github.com/acme/payments", IsPrivate: true, DefaultBranch: "main",
	})
	if err != nil {
		t.Fatalf("seed installation repo: %v", err)
	}

	rejected := fixture.request(
		t, http.MethodPost, "/api/v1/repositories/external",
		`{"full_name":"acme/payments"}`, "127.0.0.1:46001", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	// 冲突码必须指明是类型冲突：笼统的 validation_failed 会让调用方以为是入参格式问题。
	assertAPIError(t, rejected, http.StatusConflict, errorCodeRepositoryTypeConflict)

	// 原行必须保持原样：类型、同步状态、私有标记均未被覆写。
	after, err := fixture.store.Repositories().Get(ctx, seeded.ID)
	if err != nil {
		t.Fatalf("read repo: %v", err)
	}
	if after.Type != store.RepositoryTypeInstallation {
		t.Fatalf("类型被改写为 %q", after.Type)
	}
	if after.SyncStatus != store.SyncStatusActive {
		t.Fatalf("同步状态被改写为 %q", after.SyncStatus)
	}
	if !after.IsPrivate {
		t.Fatal("私有标记被抹平")
	}
	if after.DefaultBranch != "main" {
		t.Fatalf("default_branch 被抹平为 %q", after.DefaultBranch)
	}
	// 不得新建第二行。
	rows, _, err := fixture.store.Repositories().List(ctx, store.ListFilter{PerPage: 100})
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应仍只有 1 行，实际 %d", len(rows))
	}
}

// TestAddExternalRepositoryIdempotentForExistingExternal 已是外部仓时幂等返回既有行，
// 不重复计入外部仓上限、不重置同步状态。
func TestAddExternalRepositoryIdempotentForExistingExternal(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()

	seeded, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-ext-1", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "octo", Name: "pub", FullName: "octo/pub",
		HTMLURL: "https://github.com/octo/pub",
	})
	if err != nil {
		t.Fatalf("seed external repo: %v", err)
	}

	resp := fixture.request(
		t, http.MethodPost, "/api/v1/repositories/external",
		`{"full_name":"octo/pub"}`, "127.0.0.1:46002", cookies,
		map[string]string{CSRFHeaderName: csrf.Value},
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("重复添加外部仓应幂等 200，实际 %d body=%s", resp.Code, resp.Body.String())
	}
	var got store.Repository
	if err := json.Unmarshal(resp.Body.Bytes(), &got); err != nil {
		t.Fatal(err)
	}
	if got.ID != seeded.ID {
		t.Fatalf("应返回既有行 %q，实际 %q", seeded.ID, got.ID)
	}
	if got.SyncStatus != store.SyncStatusActive {
		t.Fatalf("同步状态不应被重置，实际 %q", got.SyncStatus)
	}
}

// TestUpsertRejectsReclassifyingInstallationRepo 存储层纵深防守：任何写路径都不得把
// 安装仓降级为其它类型。
func TestUpsertRejectsReclassifyingInstallationRepo(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	ctx := t.Context()
	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-guard-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "svc", FullName: "acme/svc",
	}); err != nil {
		t.Fatalf("seed: %v", err)
	}

	if _, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-guard-1", Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusBaseline,
		Owner: "acme", Name: "svc", FullName: "acme/svc",
	}); err == nil {
		t.Fatal("安装仓降级为外部仓应被拒绝")
	}
	// 原行未被改动。
	after, err := fixture.store.Repositories().GetByFullName(ctx, "acme/svc")
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if after.Type != store.RepositoryTypeInstallation || after.SyncStatus != store.SyncStatusActive {
		t.Fatalf("原行被改动：type=%q status=%q", after.Type, after.SyncStatus)
	}
}

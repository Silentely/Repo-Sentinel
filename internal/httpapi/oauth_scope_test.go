package httpapi

import (
	"crypto/sha256"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/golang-jwt/jwt/v5"
)

// signedOAuthToken 以测试密钥签发一枚指定 scope 的访问令牌，供作用域边界探针使用。
func (f *httpTestFixture) signedOAuthToken(t *testing.T, scope string) string {
	t.Helper()
	ring := oauthTestRing(t)
	derived, err := ring.DeriveHMACKey([]byte(oauthSigningAAD))
	if err != nil {
		t.Fatalf("derive: %v", err)
	}
	sum := sha256.Sum256(derived)
	key := sum[:]
	now := time.Now().UTC()
	claims := oauthClaims{
		Scope: scope,
		RegisteredClaims: jwt.RegisteredClaims{
			Issuer:    "https://reposentinel.example",
			Subject:   oauthTestClientID,
			Audience:  jwt.ClaimStrings{"https://reposentinel.example/api/v1"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(time.Hour)),
			ID:        "probe-token",
		},
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign: %v", err)
	}
	return signed
}

// TestBearerReadScopeCannotMutate 回归：声明为只读的 OAuth Agent 令牌不得执行管理写操作。
// 原实现中 oauthValidateToken 只返回 Subject、从不解析 Scope，而 mutating 组又嵌套在
// Bearer 可达的 protected 组内且 csrfMiddleware 对 Agent 直接放行，导致持「只读」客户端
// 凭据者取得删除仓库、改写系统设置、增删通知渠道等全部管理写权。
func TestBearerReadScopeCannotMutate(t *testing.T) {
	fixture := oauthFixture(t)
	fixture.bootstrapAdmin(t)
	token := mcpAccessToken(t, fixture)
	ctx := t.Context()

	// 令牌自身声明的唯一作用域必须是 read。
	payload := requestToken(t, fixture, "grant_type=client_credentials&client_id="+oauthTestClientID+
		"&client_secret="+oauthTestClientSecret)
	if scope, _ := payload["scope"].(string); !strings.Contains(scope, "read") {
		t.Fatalf("令牌 scope=%q，应含 read", scope)
	}

	seeded, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-scope-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "app", FullName: "acme/app",
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	cases := []struct {
		name, method, path, body string
	}{
		{"delete repository", http.MethodDelete, "/api/v1/repositories/" + seeded.ID, ""},
		{"put settings", http.MethodPut, "/api/v1/system/settings", `{"digest_enabled":true}`},
		{"upsert channel", http.MethodPut, "/api/v1/notifications/channels/http_webhook",
			`{"name":"probe","enabled":true,"target":"https://example.invalid/hook"}`},
		{"add external repository", http.MethodPost, "/api/v1/repositories/external", `{"full_name":"octo/probe"}`},
	}
	for _, tc := range cases {
		resp := fixture.request(t, tc.method, tc.path, tc.body, "203.0.113.9:47001", nil,
			map[string]string{"Authorization": "Bearer " + token})
		if resp.Code != http.StatusForbidden {
			t.Fatalf("%s：read 令牌应被 403 拒绝，实际 %d body=%s", tc.name, resp.Code, resp.Body.String())
		}
	}

	// 仓库行不得被删除。
	if _, err := fixture.store.Repositories().Get(ctx, seeded.ID); err != nil {
		t.Fatalf("仓库被 read 令牌删除: %v", err)
	}
	// 设置不得被写入。
	if row, err := fixture.store.Settings().Get(ctx, "digest_enabled"); err == nil {
		t.Fatalf("设置被 read 令牌写入: %s", string(row.ValueJSON))
	}
	// 渠道不得被创建。
	if rows, err := fixture.store.Channels().List(ctx); err == nil && len(rows) > 0 {
		t.Fatalf("渠道被 read 令牌创建：%d 行", len(rows))
	}
}

// TestBearerReadScopeCanStillRead 对照：同一 read 令牌的只读能力不受影响。
func TestBearerReadScopeCanStillRead(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)

	for _, path := range []string{
		"/api/v1/dashboard",
		"/api/v1/repositories",
		"/api/v1/notifications/channels",
		"/api/v1/system/settings",
	} {
		resp := fixture.request(t, http.MethodGet, path, "", "203.0.113.9:47002", nil,
			map[string]string{"Authorization": "Bearer " + token})
		if resp.Code != http.StatusOK {
			t.Fatalf("GET %s 应仍可读，实际 %d body=%s", path, resp.Code, resp.Body.String())
		}
	}
}

// TestOAuthTokenWithoutKnownScopeRejected 未声明任何已知作用域的令牌一律拒绝：
// 空 scope 不得被解释为「全部允许」。
func TestOAuthTokenWithoutKnownScopeRejected(t *testing.T) {
	fixture := oauthFixture(t)
	// 直接构造一枚无 scope 声明的令牌走 authenticationMiddleware。
	token := fixture.signedOAuthToken(t, "")
	resp := fixture.request(t, http.MethodGet, "/api/v1/dashboard", "", "203.0.113.9:47003", nil,
		map[string]string{"Authorization": "Bearer " + token})
	if resp.Code != http.StatusUnauthorized {
		t.Fatalf("无作用域令牌应 401，实际 %d body=%s", resp.Code, resp.Body.String())
	}
}

package httpapi

import (
	"bytes"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
)

const (
	oauthTestClientID     = "reposentinel-agent"
	oauthTestClientSecret = "test-agent-secret"
)

// oauthTestRing 构造测试用 KeyRing（主密钥固定，派生签名密钥稳定）。
func oauthTestRing(t *testing.T) *cryptox.KeyRing {
	t.Helper()
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(hex.EncodeToString(bytes.Repeat([]byte{0x11}, 32))),
	})
	if err != nil {
		t.Fatalf("创建测试 KeyRing 失败: %v", err)
	}
	return &ring
}

func oauthFixture(t *testing.T) *httpTestFixture {
	t.Helper()
	return newHTTPTestFixture(t, httpTestOptions{
		publicBaseURL:     "https://reposentinel.example",
		keyRing:           oauthTestRing(t),
		oauthClientID:     oauthTestClientID,
		oauthClientSecret: oauthTestClientSecret,
	})
}

// requestToken 通过 client_credentials 获取访问令牌，失败即终止测试。
func requestToken(t *testing.T, fixture *httpTestFixture, body string) map[string]any {
	t.Helper()
	response := fixture.requestWithContentType(
		t, http.MethodPost, "/oauth/token", body,
		"application/x-www-form-urlencoded", "127.0.0.1:42001", nil, nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("token 状态=%d 响应=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("token 响应不是合法 JSON: %v", err)
	}
	return payload
}

// TestOAuthToken签发并携带Bearer访问API 验证 client_credentials 全链路。
func TestOAuthToken签发并携带Bearer访问API(t *testing.T) {
	fixture := oauthFixture(t)
	tokenBody := "grant_type=client_credentials&client_id=" + oauthTestClientID +
		"&client_secret=" + oauthTestClientSecret
	payload := requestToken(t, fixture, tokenBody)

	accessToken, ok := payload["access_token"].(string)
	if !ok || accessToken == "" {
		t.Fatalf("access_token 缺失: %+v", payload)
	}
	if payload["token_type"] != "Bearer" {
		t.Fatalf("token_type=%v", payload["token_type"])
	}

	// 携带令牌访问受保护 API。
	dashboard := fixture.request(
		t, http.MethodGet, "/api/v1/dashboard", "", "127.0.0.1:42002", nil,
		map[string]string{"Authorization": "Bearer " + accessToken},
	)
	if dashboard.Code != http.StatusOK {
		t.Fatalf("Bearer 访问 dashboard 状态=%d 响应=%s", dashboard.Code, dashboard.Body.String())
	}
	var stats map[string]any
	if err := json.Unmarshal(dashboard.Body.Bytes(), &stats); err != nil {
		t.Fatalf("dashboard 响应不是合法 JSON: %v", err)
	}
	if _, ok := stats["open_issues"]; !ok {
		t.Fatalf("dashboard 响应缺少 open_issues: %+v", stats)
	}
}

// TestOAuthBearer支持BasicAuth 验证 Basic Auth 提取凭据路径。
func TestOAuthBearer支持BasicAuth(t *testing.T) {
	fixture := oauthFixture(t)
	response := fixture.requestWithContentType(
		t, http.MethodPost, "/oauth/token", "grant_type=client_credentials",
		"application/x-www-form-urlencoded", "127.0.0.1:42003", nil,
		map[string]string{"Authorization": "Basic " + base64Basic(oauthTestClientID, oauthTestClientSecret)},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("Basic Auth token 状态=%d 响应=%s", response.Code, response.Body.String())
	}
}

// TestOAuthToken拒绝非法凭据与grant 验证错误分支。
func TestOAuthToken拒绝非法凭据与grant(t *testing.T) {
	fixture := oauthFixture(t)
	cases := []struct {
		name string
		body string
		want int
	}{
		{"错误密钥", "grant_type=client_credentials&client_id=" + oauthTestClientID + "&client_secret=wrong", http.StatusUnauthorized},
		{"错误客户端", "grant_type=client_credentials&client_id=nope&client_secret=" + oauthTestClientSecret, http.StatusUnauthorized},
		{"缺失凭据", "grant_type=client_credentials", http.StatusUnauthorized},
		{"不支持grant", "grant_type=password&client_id=" + oauthTestClientID + "&client_secret=" + oauthTestClientSecret, http.StatusBadRequest},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			response := fixture.requestWithContentType(
				t, http.MethodPost, "/oauth/token", tc.body,
				"application/x-www-form-urlencoded", "127.0.0.1:42004", nil, nil,
			)
			if response.Code != tc.want {
				t.Fatalf("状态=%d 期望 %d；响应=%s", response.Code, tc.want, response.Body.String())
			}
			var payload map[string]any
			if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
				t.Fatalf("错误响应不是合法 JSON: %v", err)
			}
			if code, _ := payload["error"].(string); code == "" {
				t.Fatalf("缺少 OAuth error 字段: %+v", payload)
			}
		})
	}
}

// TestOAuth未配置时拒绝签发 未配置 client_secret 时 token 端点应 401。
func TestOAuth未配置时拒绝签发(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		publicBaseURL: "https://reposentinel.example",
		keyRing:       oauthTestRing(t),
	})
	response := fixture.requestWithContentType(
		t, http.MethodPost, "/oauth/token",
		"grant_type=client_credentials&client_id="+oauthTestClientID+"&client_secret=whatever",
		"application/x-www-form-urlencoded", "127.0.0.1:42005", nil, nil,
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("状态=%d，期望 401；响应=%s", response.Code, response.Body.String())
	}
}

// TestOAuthJWKS输出签名公钥 验证 jwks 端点。
func TestOAuthJWKS输出签名公钥(t *testing.T) {
	fixture := oauthFixture(t)
	response := fixture.request(t, http.MethodGet, "/oauth/jwks", "", "127.0.0.1:42006", nil, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("状态=%d", response.Code)
	}
	var payload struct {
		Keys []struct {
			Kty string `json:"kty"`
			Alg string `json:"alg"`
			Kid string `json:"kid"`
			K   string `json:"k"`
		} `json:"keys"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("JWKS 不是合法 JSON: %v", err)
	}
	if len(payload.Keys) != 1 || payload.Keys[0].Kty != "oct" || payload.Keys[0].Alg != "HS256" || payload.Keys[0].K == "" {
		t.Fatalf("JWKS 结构异常: %+v", payload)
	}
}

// TestOAuthBearer拒绝伪造令牌 无密钥环或非法令牌都应 401。
func TestOAuthBearer拒绝伪造令牌(t *testing.T) {
	fixture := oauthFixture(t)
	response := fixture.request(
		t, http.MethodGet, "/api/v1/dashboard", "", "127.0.0.1:42007", nil,
		map[string]string{"Authorization": "Bearer forged.invalid.token"},
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("伪造令牌状态=%d，期望 401；响应=%s", response.Code, response.Body.String())
	}
}

// TestOAuthAuthorize声明端点返回不支持 授权端点不提供交互式授权流。
func TestOAuthAuthorize声明端点返回不支持(t *testing.T) {
	fixture := oauthFixture(t)
	for _, method := range []string{http.MethodGet, http.MethodPost} {
		response := fixture.request(t, method, "/oauth/authorize", "", "127.0.0.1:42008", nil, nil)
		if response.Code != http.StatusBadRequest {
			t.Fatalf("%s /oauth/authorize 状态=%d，期望 400", method, response.Code)
		}
	}
}

func base64Basic(user, pass string) string {
	return base64.StdEncoding.EncodeToString([]byte(strings.Join([]string{user, pass}, ":")))
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// enableTOTPForTest 通过管理 API 开启 2FA，返回本次绑定的种子。
func enableTOTPForTest(t *testing.T, fixture *httpTestFixture) string {
	t.Helper()
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	setupResp := fixture.request(t, http.MethodPost, "/api/v1/admin/2fa/setup", `{}`, "127.0.0.1:44100", cookies, headers)
	var setupBody struct {
		Secret string `json:"secret"`
	}
	if err := json.Unmarshal(setupResp.Body.Bytes(), &setupBody); err != nil || setupBody.Secret == "" {
		t.Fatalf("2FA setup 失败: %s", setupResp.Body.String())
	}
	passcode, err := auth.GenerateTOTPCode(setupBody.Secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}
	enableResp := fixture.request(t, http.MethodPost, "/api/v1/admin/2fa/enable",
		`{"secret":"`+setupBody.Secret+`","passcode":"`+passcode+`"}`,
		"127.0.0.1:44101", cookies, headers)
	if enableResp.Code != http.StatusOK {
		t.Fatalf("2FA enable 失败: %s", enableResp.Body.String())
	}
	return setupBody.Secret
}

// completeTOTPLogin 走完两阶段登录（密码 + 动态码），返回动态码阶段的状态码。
func completeTOTPLogin(t *testing.T, fixture *httpTestFixture, secret, remoteAddr string) int {
	t.Helper()
	loginResp := fixture.request(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		remoteAddr, nil, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("第一因子登录失败: 状态=%d 响应=%s", loginResp.Code, loginResp.Body.String())
	}
	var body struct {
		Requires2FA bool   `json:"requires_2fa"`
		Ticket      string `json:"ticket"`
	}
	if err := json.Unmarshal(loginResp.Body.Bytes(), &body); err != nil {
		t.Fatalf("登录响应不是合法 JSON: %v", err)
	}
	if !body.Requires2FA || body.Ticket == "" {
		t.Fatalf("应进入第二因子阶段: %s", loginResp.Body.String())
	}
	passcode, err := auth.GenerateTOTPCode(secret, time.Now().UTC())
	if err != nil {
		t.Fatalf("生成动态码失败: %v", err)
	}
	resp := fixture.request(t, http.MethodPost, "/api/v1/auth/login/2fa",
		`{"ticket":"`+body.Ticket+`","passcode":"`+passcode+`"}`,
		remoteAddr, nil, nil)
	return resp.Code
}

// TestTOTPLoginUsesSeparateRateLimitBucket 回归：第二因子按独立令牌桶限流。
// 共用 LoginLimiter 时一次完整登录消耗两份额度（每桶每分钟 5 个），
// 单 IP 每分钟可完成的登录数凭空减半，第 3 次完整登录即被 429 拦下。
func TestTOTPLoginUsesSeparateRateLimitBucket(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)
	secret := enableTOTPForTest(t, fixture)

	const remoteAddr = "203.0.113.40:45001"
	for attempt := 1; attempt <= 3; attempt++ {
		if status := completeTOTPLogin(t, fixture, secret, remoteAddr); status != http.StatusOK {
			t.Fatalf("第 %d 次完整登录应成功（同一 IP 同一窗口），实际状态 %d", attempt, status)
		}
	}
}

// TestTOTPLoginStillBoundedByOwnBucket 动态码路径仍有自己的上界：5 次尝试后第 6 次 429，
// 且第一因子额度不被它消耗。
func TestTOTPLoginStillBoundedByOwnBucket(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)
	enableTOTPForTest(t, fixture)

	const remoteAddr = "203.0.113.41:45002"
	for attempt := 1; attempt <= 5; attempt++ {
		resp := fixture.request(t, http.MethodPost, "/api/v1/auth/login/2fa",
			`{"ticket":"not-a-ticket","passcode":"000000"}`, remoteAddr, nil, nil)
		if resp.Code != http.StatusUnauthorized {
			t.Fatalf("第 %d 次动态码尝试应为 401（票据无效），实际 %d body=%s", attempt, resp.Code, resp.Body.String())
		}
	}
	limited := fixture.request(t, http.MethodPost, "/api/v1/auth/login/2fa",
		`{"ticket":"not-a-ticket","passcode":"000000"}`, remoteAddr, nil, nil)
	assertAPIError(t, limited, http.StatusTooManyRequests, errorCodeRateLimited)

	// 分桶的直接证据：动态码额度耗尽后，第一因子登录仍可正常开始。
	loginResp := fixture.request(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		remoteAddr, nil, nil)
	if loginResp.Code != http.StatusOK {
		t.Fatalf("第一因子不应受动态码限流影响，实际 %d body=%s", loginResp.Code, loginResp.Body.String())
	}
}

// TestLoginWithLegacyPlaintextTOTPRowReturnsActionableError 回归：库内是历史明文种子的
// 2FA 配置时，登录不得退化成 500 internal_error，也不能只说「用户名或密码错误」，
// 而要给出可执行的恢复指引。
func TestLoginWithLegacyPlaintextTOTPRowReturnsActionableError(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)

	// 直接写入历史形状的明文行（SaveTOTPConfig 已不再产生这种行）。
	if _, err := fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s-totp-legacy",
		Key:       auth.TOTPSettingKey,
		ValueJSON: []byte(`{"enabled":true,"plain_secret":"JBSWY3DPEHPK3PXP","updated_at":"2026-01-01T00:00:00Z"}`),
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "test",
	}); err != nil {
		t.Fatalf("写入历史明文行失败: %v", err)
	}

	resp := fixture.request(t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		"203.0.113.42:45003", nil, nil)
	assertAPIError(t, resp, http.StatusServiceUnavailable, errorCodeTOTPConfigUnreadable)

	var body struct {
		Message string `json:"message"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v", err)
	}
	if !strings.Contains(body.Message, "reset-2fa") {
		t.Fatalf("错误文案应指明 CLI 恢复手段，实际 %q", body.Message)
	}
}

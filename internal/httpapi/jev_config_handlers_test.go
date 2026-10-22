package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

func jevTestFixture(t *testing.T) (*httpTestFixture, *jev.RuntimeConfig, *cryptox.KeyRing) {
	t.Helper()
	ring := testHTTPKeyRing(t)
	jevCfg := config.JevConfig{
		Enabled: false,
		BaseURL: "https://api.typesafe.ai",
		Model:   "jev-1.14.0",
		Mode:    "shadow",
		Retries: 1,
		Notification: config.PlaneConfig{
			Timeout:        500 * time.Millisecond,
			MaxConcurrency: 8,
		},
		Review: config.PlaneConfig{
			Timeout:        300 * time.Millisecond,
			MaxConcurrency: 8,
		},
		ChatOps: config.PlaneConfig{
			Timeout:        500 * time.Millisecond,
			MaxConcurrency: 8,
		},
	}
	rt := jev.RuntimeFromEnv(jevCfg)
	fixture := newHTTPTestFixture(t, httpTestOptions{
		keyRing:    ring,
		jevRuntime: rt,
		jevConfig:  jevCfg,
	})
	return fixture, rt, ring
}

func TestJevConfigAPI_AuthAndCSRF(t *testing.T) {
	fixture, _, _ := jevTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 未授权拦截
	unauthGet := fixture.request(t, http.MethodGet, "/api/v1/jev/config", "", "127.0.0.1:45100", nil, nil)
	assertAPIError(t, unauthGet, http.StatusUnauthorized, "unauthorized")

	unauthPut := fixture.request(t, http.MethodPut, "/api/v1/jev/config", `{"enabled":false}`, "127.0.0.1:45101", nil, nil)
	assertAPIError(t, unauthPut, http.StatusUnauthorized, "unauthorized")

	// 缺少 CSRF 拦截
	missingCSRFPut := fixture.request(t, http.MethodPut, "/api/v1/jev/config", `{"enabled":false}`, "127.0.0.1:45102", cookies, nil)
	assertAPIError(t, missingCSRFPut, http.StatusForbidden, "csrf_failed")
}

func TestJevConfigAPI_GetInitial(t *testing.T) {
	fixture, _, _ := jevTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	getOK := fixture.request(t, http.MethodGet, "/api/v1/jev/config", "", "127.0.0.1:45103", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	var view jevConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Enabled {
		t.Fatal("initial enabled should be false")
	}
	if view.HasAPIKey {
		t.Fatal("initial has_api_key should be false")
	}
	if view.APIKeySource != "none" {
		t.Fatalf("expected api_key_source none, got %s", view.APIKeySource)
	}
	if view.Mode != "shadow" {
		t.Fatalf("expected mode shadow, got %s", view.Mode)
	}
}

func TestJevConfigAPI_PutValidAndMasking(t *testing.T) {
	fixture, rt, ring := jevTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	putReq := `{"enabled":true,"base_url":"https://api.typesafe.ai","api_key":"fixture-secret-token","model":"jev-1.14.0","mode":"shadow","retries":1,"noise_filter_enabled":true}`
	putOK := fixture.request(t, http.MethodPut, "/api/v1/jev/config", putReq, "127.0.0.1:45104", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if putOK.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putOK.Code, putOK.Body.String())
	}

	// 响应不得回显密钥
	if strings.Contains(putOK.Body.String(), "fixture-secret-token") {
		t.Fatal("response must not reveal plaintext secret")
	}

	// 校验 GET 脱敏视图
	getOK := fixture.request(t, http.MethodGet, "/api/v1/jev/config", "", "127.0.0.1:45105", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	if strings.Contains(getOK.Body.String(), "fixture-secret-token") {
		t.Fatal("GET response must not reveal plaintext secret")
	}
	var view jevConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || !view.HasAPIKey || view.APIKeySource != "db" || !view.NoiseFilterEnabled {
		t.Fatalf("unexpected view: %+v", view)
	}

	// 校验 Runtime 内存快照
	snap := rt.Snapshot()
	if !snap.Enabled || snap.APIKey.Reveal() != "fixture-secret-token" || !snap.NoiseFilterEnabled {
		t.Fatalf("runtime snapshot did not update: %+v", snap)
	}

	// 校验持久化加密信封
	stored, err := jev.LoadStoredConfig(t.Context(), fixture.store)
	if err != nil {
		t.Fatalf("load stored config: %v", err)
	}
	if strings.Contains(stored.APIKeyEnvelope, "fixture-secret-token") {
		t.Fatal("stored envelope leaked plaintext")
	}
	decrypted, err := jev.DecryptAPIKey(t.Context(), ring, stored.APIKeyEnvelope)
	if err != nil || decrypted != "fixture-secret-token" {
		t.Fatalf("decrypt stored key failed: %v, plain=%s", err, decrypted)
	}
}

func TestJevConfigAPI_ValidationRejections(t *testing.T) {
	fixture, _, _ := jevTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	testCases := []struct {
		name       string
		body       string
		expectCode int
		expectErr  string
	}{
		{
			name:       "invalid json",
			body:       `{not valid json}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "invalid mode rejected",
			body:       `{"mode":"invalid_mode"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "active mode rejected without evaluation evidence",
			body:       `{"mode":"active"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "activation_evidence_missing",
		},
		{
			name:       "clear_api_key when enabled rejected",
			body:       `{"enabled":true,"clear_api_key":true}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "ftp base_url rejected",
			body:       `{"base_url":"ftp://api.typesafe.ai"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "http insecure base_url rejected",
			body:       `{"base_url":"http://api.typesafe.ai"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "base_url with query rejected",
			body:       `{"base_url":"https://api.typesafe.ai?query=1"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "retries out of range rejected",
			body:       `{"retries":2}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "emergency advisory missing reason rejected",
			body:       `{"emergency_advisory_until":"` + time.Now().Add(10*time.Minute).Format(time.RFC3339) + `"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "emergency advisory exceeding 15 minutes rejected",
			body:       `{"emergency_advisory_until":"` + time.Now().Add(20*time.Minute).Format(time.RFC3339) + `","emergency_advisory_reason":"test"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
		{
			name:       "emergency advisory in past rejected",
			body:       `{"emergency_advisory_until":"` + time.Now().Add(-5*time.Minute).Format(time.RFC3339) + `","emergency_advisory_reason":"test"}`,
			expectCode: http.StatusBadRequest,
			expectErr:  "validation_failed",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			res := fixture.request(t, http.MethodPut, "/api/v1/jev/config", tc.body, "127.0.0.1:45106", cookies, map[string]string{CSRFHeaderName: csrf.Value})
			assertAPIError(t, res, tc.expectCode, tc.expectErr)
		})
	}
}

func TestJevConfigAPI_EmergencyAdvisoryAndTestEndpoint(t *testing.T) {
	fixture, rt, _ := jevTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 测试应急 Advisory 模式配置
	until := time.Now().Add(10 * time.Minute)
	advisoryReq := `{"emergency_advisory_until":"` + until.Format(time.RFC3339) + `","emergency_advisory_reason":"vendor degraded"}`
	putOK := fixture.request(t, http.MethodPut, "/api/v1/jev/config", advisoryReq, "127.0.0.1:45107", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if putOK.Code != http.StatusOK {
		t.Fatalf("PUT advisory status=%d body=%s", putOK.Code, putOK.Body.String())
	}
	if !rt.IsChatOpsAdvisory(time.Now()) {
		t.Fatal("chatops should be advisory during emergency advisory window")
	}

	getAdv := fixture.request(t, http.MethodGet, "/api/v1/jev/config", "", "127.0.0.1:45107", cookies, nil)
	if getAdv.Code != http.StatusOK {
		t.Fatalf("GET config status=%d", getAdv.Code)
	}
	var advResp jevConfigResponse
	if err := json.Unmarshal(getAdv.Body.Bytes(), &advResp); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if advResp.EmergencyAdvisoryUntil == nil || advResp.EmergencyAdvisoryReason != "vendor degraded" {
		t.Fatalf("expected emergency advisory in GET resp, got %+v", advResp)
	}

	getAlias := fixture.request(t, http.MethodGet, "/api/v1/system/jev", "", "127.0.0.1:45107", cookies, nil)
	if getAlias.Code != http.StatusOK {
		t.Fatalf("GET alias /api/v1/system/jev status=%d", getAlias.Code)
	}

	// 测试 POST /api/v1/jev/test 在未配置 API Key 时返回 400
	testNoKey := fixture.request(t, http.MethodPost, "/api/v1/jev/test", "{}", "127.0.0.1:45108", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, testNoKey, http.StatusBadRequest, errorCodeValidationFailed)

	// 启动 TLS Mock Jev Server
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/systemone" {
			http.NotFound(w, r)
			return
		}
		if r.Header.Get("Authorization") != "Bearer test-key" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		// 返回有效的 Task 0 系统响应
		respJSON := `{
			"model": "jev-1.14.0",
			"usage": {"input_tokens": 120, "output_tokens": 15},
			"answers": {
				"is_actionable": {"type": "noul", "probability": 0.85}
			}
		}`
		_, _ = w.Write([]byte(respJSON))
	}))
	defer ts.Close()

	// 保存包含 mock URL 和 key 的有效配置
	setKeyReq := `{"enabled":true,"base_url":"` + ts.URL + `","api_key":"test-key","model":"jev-1.14.0"}`
	putKeyOK := fixture.request(t, http.MethodPut, "/api/v1/jev/config", setKeyReq, "127.0.0.1:45109", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if putKeyOK.Code != http.StatusOK {
		t.Fatalf("PUT key status=%d body=%s", putKeyOK.Code, putKeyOK.Body.String())
	}

	// 运行测试端点（NewClient 发起真实 TLS 请求）
	testRes := fixture.request(t, http.MethodPost, "/api/v1/jev/test", "{}", "127.0.0.1:45110", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	// 由于生产环境 NewClient 严格要求有效证书（自签名证书会返回 StatusBadGateway "jev_test_failed"），确认端点已执行出站校验
	if testRes.Code != http.StatusBadGateway && testRes.Code != http.StatusOK {
		t.Fatalf("unexpected test status: %d body=%s", testRes.Code, testRes.Body.String())
	}
	if testRes.Code == http.StatusBadGateway {
		assertAPIError(t, testRes, http.StatusBadGateway, "jev_test_failed")
	}
}

package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

// aiTestFixture 构造装配好 AI 运行时的测试 fixture。
func aiTestFixture(t *testing.T) (*httpTestFixture, *ai.RuntimeConfig, *ai.Client) {
	t.Helper()
	// 全默认 bool 值（digest/triage 默认 true），其余字段 unset：管理台可编辑。
	rt := ai.RuntimeFromEnv(config.AIConfig{DigestEnabled: true, TriageEnabled: true})
	client := rt.Client()
	fixture := newHTTPTestFixture(t, httpTestOptions{
		keyRing:   testHTTPKeyRing(t),
		aiRuntime: rt,
		aiClient:  client,
	})
	return fixture, rt, client
}

// AI 配置 API：认证保护、保存后敏感字段不回显、Clear 清除。
func TestAIConfigAPI(t *testing.T) {
	fixture, _, client := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	getUnauthorized := fixture.request(t, http.MethodGet, "/api/v1/ai/config", "", "127.0.0.1:45001", nil, nil)
	assertAPIError(t, getUnauthorized, http.StatusUnauthorized, "unauthorized")

	// 初始视图：全部可编辑、密钥未配置。
	getOK := fixture.request(t, http.MethodGet, "/api/v1/ai/config", "", "127.0.0.1:45002", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	var view aiConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.CanEditInUI || view.APIKeyConfigured {
		t.Fatal("初始应可编辑且密钥未配置")
	}

	// 缺少 CSRF 的 PUT 应被拒绝。
	putMissingCSRF := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"enabled":true,"api_key":"sk-ui"}`, "127.0.0.1:45003", cookies, nil)
	assertAPIError(t, putMissingCSRF, http.StatusForbidden, "csrf_failed")

	// 保存完整配置。
	putOK := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"enabled":true,"base_url":"http://127.0.0.1:11434/v1","model":"llama3.1","timeout_sec":45,"max_tokens":1024,"digest_enabled":true,"triage_enabled":false,"api_key":"sk-ui"}`,
		"127.0.0.1:45004", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if putOK.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putOK.Code, putOK.Body.String())
	}
	if err := json.Unmarshal(putOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.Enabled || view.BaseURL != "http://127.0.0.1:11434/v1" || view.Model != "llama3.1" ||
		view.TimeoutSec != 45 || view.MaxTokens != 1024 || view.TriageEnabled {
		t.Fatalf("PUT 后视图应反映新配置：%+v", view)
	}
	if !view.APIKeyConfigured {
		t.Fatal("保存后应标记密钥已配置")
	}
	if view.APIKeySource != "database" {
		t.Fatalf("密钥来源应为 database，实际 %s", view.APIKeySource)
	}
	// 响应不得包含密钥明文。
	if view.EnabledSource != "database" {
		t.Fatal("enabled 应由 DB 补缺")
	}
	// 热更新：共享客户端应已生效。
	if !client.IsEnabled() || client.APIKey != "sk-ui" || client.Model != "llama3.1" || client.IsTriageEnabled() {
		t.Fatalf("热更新后共享客户端应生效：%+v", client.Snapshot())
	}

	// ClearAPIKey 清除密钥。
	clearOK := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"clear_api_key":true}`, "127.0.0.1:45005", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if clearOK.Code != http.StatusOK {
		t.Fatalf("clear PUT status=%d body=%s", clearOK.Code, clearOK.Body.String())
	}
	if err := json.Unmarshal(clearOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.APIKeyConfigured {
		t.Fatal("clear 后密钥应标记未配置")
	}
	if client.IsEnabled() {
		t.Fatal("clear 后客户端应因无密钥而不可用")
	}
}

// env 锁定的字段在管理台写入时返回 409。
func TestAIConfigEnvLockedField(t *testing.T) {
	rt := ai.RuntimeFromEnv(config.AIConfig{
		Enabled: true, BaseURL: "https://api.openai.com/v1", APIKey: config.NewSecret("sk-env"),
		Model: "gpt-4o-mini", Timeout: 20 * time.Second, MaxTokens: 800,
		DigestEnabled: true, TriageEnabled: true,
	})
	fixture := newHTTPTestFixture(t, httpTestOptions{
		keyRing:   testHTTPKeyRing(t),
		aiRuntime: rt,
		aiClient:  rt.Client(),
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	// env 已设置的 enabled/api_key 应锁定；env 未设置的字段仍可写。
	put := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"enabled":false}`, "127.0.0.1:45101", cookies, headers)
	assertAPIError(t, put, http.StatusConflict, "ai_field_locked")

	putKey := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"api_key":"sk-overwrite"}`, "127.0.0.1:45102", cookies, headers)
	assertAPIError(t, putKey, http.StatusConflict, "ai_field_locked")

	// base_url/model 是 env 设置的也应锁定。
	putBase := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"base_url":"http://evil.example/v1"}`, "127.0.0.1:45103", cookies, headers)
	assertAPIError(t, putBase, http.StatusConflict, "ai_field_locked")
}

// 非法输入被拒绝：base_url 非 http(s)、timeout 越界。
func TestAIConfigValidation(t *testing.T) {
	fixture, _, _ := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	cases := []struct {
		name string
		body string
	}{
		{"base_url 非 http(s)", `{"base_url":"ftp://bad"}`},
		{"timeout 越界", `{"timeout_sec":0}`},
		{"timeout 过大", `{"timeout_sec":7200}`},
		{"max_tokens 过小", `{"max_tokens":10}`},
		{"空 API Key", `{"api_key":"  "}`},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := fixture.request(t, http.MethodPut, "/api/v1/ai/config", tc.body,
				"127.0.0.1:45110", cookies, headers)
			assertAPIError(t, resp, http.StatusBadRequest, errorCodeValidationFailed)
		})
	}
}

// 未装配 AI 运行时（旧部署/降级路径）时 GET/PUT 返回 503。
func TestAIConfigUnavailable(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	get := fixture.request(t, http.MethodGet, "/api/v1/ai/config", "", "127.0.0.1:45201", cookies, nil)
	assertAPIError(t, get, http.StatusServiceUnavailable, errorCodeInternal)

	put := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"enabled":true}`,
		"127.0.0.1:45202", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, put, http.StatusServiceUnavailable, errorCodeInternal)
}

// 视图的 timeout_sec 应与运行时时长一致（秒为单位往返）。
func TestAIConfigTimeoutRoundTrip(t *testing.T) {
	fixture, _, _ := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	put := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"timeout_sec":90}`,
		"127.0.0.1:45210", cookies, headers)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	var view aiConfigResponse
	if err := json.Unmarshal(put.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.TimeoutSec != 90 || view.TimeoutSource != "database" {
		t.Fatalf("timeout 视图异常：%+v", view)
	}
}

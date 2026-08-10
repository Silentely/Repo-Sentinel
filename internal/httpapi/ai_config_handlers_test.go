package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
)

// aiTestFixture 构造装配好 AI 运行时的测试 fixture。
func aiTestFixture(t *testing.T) (*httpTestFixture, *ai.RuntimeConfig, *ai.Client) {
	t.Helper()
	// 全默认 bool 值（digest/triage 默认 true）与 Retries 默认 1，其余字段 unset：管理台可编辑。
	// aiConfig 与 aiRuntime 同源，保证 PUT 热更新（RuntimeFromEnv(Config.AI)）不把默认值误判为 env 锁定。
	aiCfg := config.AIConfig{Retries: 1, DigestEnabled: true, TriageEnabled: true}
	rt := ai.RuntimeFromEnv(aiCfg)
	client := rt.Client()
	fixture := newHTTPTestFixture(t, httpTestOptions{
		keyRing:   testHTTPKeyRing(t),
		aiRuntime: rt,
		aiClient:  client,
		aiConfig:  aiCfg,
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
		Model: "gpt-4o-mini", Timeout: 20 * time.Second, MaxTokens: 800, Retries: 3,
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

	// retries 是 env 显式设置的也应锁定。
	putRetries := fixture.request(t, http.MethodPut, "/api/v1/ai/config",
		`{"retries":1}`, "127.0.0.1:45104", cookies, headers)
	assertAPIError(t, putRetries, http.StatusConflict, "ai_field_locked")
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

	test := fixture.request(t, http.MethodPost, "/api/v1/ai/test", `{}`,
		"127.0.0.1:45203", cookies, map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, test, http.StatusServiceUnavailable, errorCodeInternal)
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

// 视图的 retries 应与运行时时长一致（含 0 的显式值往返；越界 400）。
func TestAIConfigRetriesRoundTrip(t *testing.T) {
	fixture, _, _ := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	put := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"retries":3}`,
		"127.0.0.1:45211", cookies, headers)
	if put.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", put.Code, put.Body.String())
	}
	var view aiConfigResponse
	if err := json.Unmarshal(put.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Retries != 3 || view.RetriesSource != "database" {
		t.Fatalf("retries 视图异常：%+v", view)
	}

	// 0（不重试）为合法显式值。
	zero := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"retries":0}`,
		"127.0.0.1:45212", cookies, headers)
	if zero.Code != http.StatusOK {
		t.Fatalf("retries=0 应合法：status=%d body=%s", zero.Code, zero.Body.String())
	}
	if err := json.Unmarshal(zero.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.Retries != 0 {
		t.Fatalf("retries=0 应保留显式值：%+v", view)
	}

	// 越界拒绝。
	bad := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"retries":6}`,
		"127.0.0.1:45213", cookies, headers)
	assertAPIError(t, bad, http.StatusBadRequest, errorCodeValidationFailed)
	neg := fixture.request(t, http.MethodPut, "/api/v1/ai/config", `{"retries":-1}`,
		"127.0.0.1:45214", cookies, headers)
	assertAPIError(t, neg, http.StatusBadRequest, errorCodeValidationFailed)
}

// 连通性测试：可达端点返回 ok=true，未配置 Key / 远端错误返回 ok=false，非法覆盖值 400。
func TestAIConnectivityTest(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer srv.Close()

	fixture, _, _ := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	// 未配置 Key：200 ok=false，提示需先配置。
	noKey := fixture.request(t, http.MethodPost, "/api/v1/ai/test", `{}`, "127.0.0.1:45301", cookies, headers)
	if noKey.Code != http.StatusOK {
		t.Fatalf("noKey status=%d body=%s", noKey.Code, noKey.Body.String())
	}
	var res aiTestResponse
	if err := json.Unmarshal(noKey.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Message, "API Key") {
		t.Fatalf("未配置 Key 应返回 ok=false 与提示：%+v", res)
	}

	// 携带覆盖值命中本地桩端点：ok=true，回显实际测试的 model/base_url。
	okReq := fixture.request(t, http.MethodPost, "/api/v1/ai/test",
		fmt.Sprintf(`{"base_url":%q,"model":"probe-model","api_key":"sk-probe","timeout_sec":10,"max_tokens":100}`, srv.URL),
		"127.0.0.1:45302", cookies, headers)
	if okReq.Code != http.StatusOK {
		t.Fatalf("okReq status=%d body=%s", okReq.Code, okReq.Body.String())
	}
	if err := json.Unmarshal(okReq.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.Model != "probe-model" || res.BaseURL != srv.URL || res.LatencyMS < 0 {
		t.Fatalf("覆盖值应命中测试端点并返回 ok=true：%+v", res)
	}

	// 远端 401：200 ok=false。
	badSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
	}))
	defer badSrv.Close()
	failReq := fixture.request(t, http.MethodPost, "/api/v1/ai/test",
		fmt.Sprintf(`{"base_url":%q,"api_key":"sk-bad"}`, badSrv.URL),
		"127.0.0.1:45303", cookies, headers)
	if failReq.Code != http.StatusOK {
		t.Fatalf("failReq status=%d body=%s", failReq.Code, failReq.Body.String())
	}
	if err := json.Unmarshal(failReq.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Message, "失败") {
		t.Fatalf("远端错误应返回 ok=false：%+v", res)
	}

	// 非法覆盖值：400。
	invalidCases := []struct {
		name string
		body string
	}{
		{"base_url 非 http(s)", `{"base_url":"ftp://bad"}`},
		{"timeout 越界", `{"timeout_sec":0}`},
		{"max_tokens 过小", `{"max_tokens":10}`},
		{"retries 越界", `{"retries":6}`},
		{"retries 负数", `{"retries":-1}`},
	}
	for _, tc := range invalidCases {
		t.Run(tc.name, func(t *testing.T) {
			resp := fixture.request(t, http.MethodPost, "/api/v1/ai/test", tc.body, "127.0.0.1:45304", cookies, headers)
			assertAPIError(t, resp, http.StatusBadRequest, errorCodeValidationFailed)
		})
	}
}

// env 锁定的字段不可被测试请求覆盖：仍以环境变量值探测。
func TestAIConnectivityEnvLocked(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"OK"}}]}`))
	}))
	defer srv.Close()

	rt := ai.RuntimeFromEnv(config.AIConfig{
		Enabled: true, BaseURL: srv.URL, APIKey: config.NewSecret("sk-env"),
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

	// 尝试覆盖 base_url/api_key 应被忽略：仍命中 env 端点并返回 ok=true。
	resp := fixture.request(t, http.MethodPost, "/api/v1/ai/test",
		`{"base_url":"http://evil.example/v1","api_key":"sk-override"}`,
		"127.0.0.1:45305", cookies, headers)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var res aiTestResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if !res.OK || res.BaseURL != srv.URL {
		t.Fatalf("env 锁定字段应忽略覆盖值：%+v", res)
	}
}

// 探测超时：端点无响应时在探测上限内返回 ok=false 与友好超时提示，
// 而不是把 handler 拖过 HTTP Server WriteTimeout 导致连接被反代掐断。
func TestAIConnectivityProbeTimeout(t *testing.T) {
	slow := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(2 * time.Second)
	}))
	defer slow.Close()

	oldMax := aiTestProbeMax
	aiTestProbeMax = 200 * time.Millisecond
	defer func() { aiTestProbeMax = oldMax }()

	fixture, _, _ := aiTestFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	start := time.Now()
	resp := fixture.request(t, http.MethodPost, "/api/v1/ai/test",
		fmt.Sprintf(`{"base_url":%q,"api_key":"sk-probe"}`, slow.URL),
		"127.0.0.1:45306", cookies, headers)
	if resp.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", resp.Code, resp.Body.String())
	}
	var res aiTestResponse
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if res.OK || !strings.Contains(res.Message, "超时") {
		t.Fatalf("挂起端点应返回超时提示：%+v", res)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("探测应在上限内返回，实际耗时 %s", elapsed)
	}
}

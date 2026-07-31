package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
)

func Test健康检查区分存活与就绪且不泄漏失败原因(t *testing.T) {
	ready := &mutableReadyChecker{err: errors.New("postgres://admin:secret@example/database")}
	fixture := newHTTPTestFixture(t, httpTestOptions{ready: ready})

	live := fixture.request(t, http.MethodGet, "/health/live", "", "127.0.0.1:43001", nil, nil)
	if live.Code != http.StatusOK {
		t.Fatalf("依赖未就绪时 live 状态=%d，响应=%s", live.Code, live.Body.String())
	}
	notReady := fixture.request(t, http.MethodGet, "/health/ready", "", "127.0.0.1:43002", nil, nil)
	if notReady.Code != http.StatusServiceUnavailable {
		t.Fatalf("ready 状态=%d，期望 503；响应=%s", notReady.Code, notReady.Body.String())
	}
	if strings.Contains(notReady.Body.String(), "secret") || strings.Contains(notReady.Body.String(), "postgres://") {
		t.Fatalf("readiness 响应泄漏依赖原因: %s", notReady.Body.String())
	}

	ready.Set(nil)
	available := fixture.request(t, http.MethodGet, "/health/ready", "", "127.0.0.1:43003", nil, nil)
	if available.Code != http.StatusOK {
		t.Fatalf("依赖就绪后 ready 状态=%d，响应=%s", available.Code, available.Body.String())
	}
}

func Test版本API需要认证并返回显式构建数据库与Schema信息(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)

	unauthorized := fixture.request(t, http.MethodGet, "/api/v1/system/version", "", "127.0.0.1:43101", nil, nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized")
	cookies := fixture.login(t, httpTestPassword)
	response := fixture.request(t, http.MethodGet, "/api/v1/system/version", "", "127.0.0.1:43102", cookies, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("version 状态=%d，响应=%s", response.Code, response.Body.String())
	}
	var body map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 version JSON 失败: %v", err)
	}
	want := map[string]any{
		"version":              "0.1.0",
		"git_sha":              "0123456789abcdef",
		"git_branch":           "feature/reposentinel-mvp",
		"build_time":           "2026-07-27T12:00:00Z",
		"build_channel":        "test",
		"go_version":           "go1.26.4",
		"database_driver":      "sqlite",
		"schema_version":       "202607270001",
		"update_check_enabled": false, // fixture 默认关闭远程检查
	}
	for key, expected := range want {
		if body[key] != expected {
			t.Fatalf("version[%s]=%v，期望 %v；完整响应=%v", key, body[key], expected, body)
		}
	}
	github, ok := body["github"].(map[string]any)
	if !ok {
		t.Fatalf("version 缺少 github 状态对象：%v", body)
	}
	if path, _ := github["webhook_path"].(string); path != "/webhooks/github" {
		t.Fatalf("webhook_path=%v，期望 /webhooks/github", github["webhook_path"])
	}
	for _, key := range []string{
		"app_id_configured",
		"client_id_configured",
		"private_key_configured",
		"webhook_secret_configured",
		"external_pat_configured",
	} {
		if _, exists := github[key]; !exists {
			t.Fatalf("github 缺少字段 %s：%v", key, github)
		}
	}
}

func Test版本检查API需要认证与CSRF并返回softFail结果(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	unauthorized := fixture.request(t, http.MethodPost, "/api/v1/system/version/check", `{}`, "127.0.0.1:43111", nil, nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized")

	missingCSRF := fixture.request(t, http.MethodPost, "/api/v1/system/version/check?force=true", `{}`, "127.0.0.1:43112", cookies, nil)
	assertAPIError(t, missingCSRF, http.StatusForbidden, "csrf_failed")

	// 关闭远程检查：应 soft-fail 且 enabled=false。
	// 重新装配 handler 代价高，直接用默认零值 Config（UpdateCheck.Enabled=false）。
	response := fixture.request(
		t, http.MethodPost, "/api/v1/system/version/check?force=true", `{}`,
		"127.0.0.1:43113", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if response.Code != http.StatusOK {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
	var body versionCheckResponse
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatal(err)
	}
	if body.Version.Version != "0.1.0" {
		t.Fatalf("version=%q", body.Version.Version)
	}
	if body.UpdateCheck.Enabled {
		t.Fatal("fixture 默认应关闭或未开启远程检查")
	}
}

// roundTripperFunc 将函数适配为 http.RoundTripper，用于注入可观测的假传输层。
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func Test版本检查端点不改写装配Checker的Current(t *testing.T) {
	// 保护行为：handleVersionCheck 使用装配的共享 UpdateChecker 时不得覆写其 Current
	// （历史 bug：每次请求把本地版本写进共享 checker.Current，并发请求下构成数据竞争，
	// 修复见 system_handlers.go 中"不得写共享 Checker"注释）。
	// 注入启用了远程检查的 Checker：其 HTTPClient 传输层记录命中并立即失败
	// （Check 对此 soft-fail，不触网、结果确定性）。命中数>0 证明 handler 走的是
	// 装配的 Checker 而非临时回退分支；连续两次请求后 Current 必须保持装配基线值。
	const baselineCurrent = "0.0.0-fixture-baseline"
	var transportHits int
	checker := &updatecheck.Checker{
		Enabled:  true,
		Current:  baselineCurrent,
		CheckURL: "https://example.com/api/releases/latest",
		HTTPClient: &http.Client{Transport: roundTripperFunc(func(*http.Request) (*http.Response, error) {
			transportHits++
			return nil, errors.New("fixture: 测试环境禁网")
		})},
	}
	fixture := newHTTPTestFixture(t, httpTestOptions{updateChecker: checker})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	// 路由以 server.go 为准：POST /api/v1/system/version/check（CSRF 保护的写端点）。
	for i, remoteAddr := range []string{"127.0.0.1:43121", "127.0.0.1:43122"} {
		resp := fixture.request(
			t, http.MethodPost, "/api/v1/system/version/check?force=true", `{}`,
			remoteAddr, cookies, headers,
		)
		if resp.Code != http.StatusOK {
			t.Fatalf("第 %d 次版本检查 status=%d，响应=%s", i+1, resp.Code, resp.Body.String())
		}
	}
	if transportHits == 0 {
		t.Fatal("装配的 UpdateChecker 未被使用（传输层零命中）")
	}
	if checker.Current != baselineCurrent {
		t.Fatalf("handler 改写了共享 Checker.Current：期望保持 %q，实际 %q", baselineCurrent, checker.Current)
	}
}

type mutableReadyChecker struct {
	mu  sync.RWMutex
	err error
}

func (c *mutableReadyChecker) Ready(context.Context) error {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.err
}

func (c *mutableReadyChecker) Set(err error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.err = err
}

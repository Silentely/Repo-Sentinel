package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"strings"
	"sync"
	"testing"
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

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
	var body map[string]string
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("解析 version JSON 失败: %v", err)
	}
	want := map[string]string{
		"version":         "0.1.0",
		"git_sha":         "0123456789abcdef",
		"git_branch":      "feature/reposentinel-mvp",
		"build_time":      "2026-07-27T12:00:00Z",
		"build_channel":   "test",
		"go_version":      "go1.26.4",
		"database_driver": "sqlite",
		"schema_version":  "202607270001",
	}
	for key, expected := range want {
		if body[key] != expected {
			t.Fatalf("version[%s]=%q，期望 %q；完整响应=%v", key, body[key], expected, body)
		}
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

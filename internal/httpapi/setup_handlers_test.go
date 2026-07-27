package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
)

func TestSetup状态只在管理员不存在时标记Required(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	before := fixture.request(t, http.MethodGet, "/api/v1/setup/status", "", "127.0.0.1:41001", nil, nil)
	if before.Code != http.StatusOK {
		t.Fatalf("setup status 状态=%d，响应=%s", before.Code, before.Body.String())
	}
	var beforeBody struct {
		Required bool `json:"required"`
	}
	if err := json.Unmarshal(before.Body.Bytes(), &beforeBody); err != nil || !beforeBody.Required {
		t.Fatalf("首次 setup status=(%+v, %v)，期望 required=true", beforeBody, err)
	}

	fixture.bootstrapAdmin(t)
	after := fixture.request(t, http.MethodGet, "/api/v1/setup/status", "", "127.0.0.1:41002", nil, nil)
	var afterBody struct {
		Required bool `json:"required"`
	}
	if err := json.Unmarshal(after.Body.Bytes(), &afterBody); err != nil || afterBody.Required {
		t.Fatalf("已有管理员 setup status=(%+v, %v)，期望 required=false", afterBody, err)
	}
}

func TestSetup默认拒绝远端请求(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	response := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/setup",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		"203.0.113.10:41101",
		nil,
		map[string]string{"X-Forwarded-For": "127.0.0.1"},
	)
	assertAPIError(t, response, http.StatusForbidden, "forbidden")
}

func TestSetup显式允许远端后创建管理员与安全Cookie(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{allowRemote: true})
	response := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/setup",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		"203.0.113.10:41201",
		nil,
		nil,
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("远端显式允许 setup 状态=%d，响应=%s", response.Code, response.Body.String())
	}
	cookies := response.Result().Cookies()
	assertAuthCookies(t, cookies)

	session := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "203.0.113.10:41202", cookies, nil)
	if session.Code != http.StatusOK {
		t.Fatalf("setup 后 Session 状态=%d，响应=%s", session.Code, session.Body.String())
	}
	status := fixture.request(t, http.MethodGet, "/api/v1/setup/status", "", "203.0.113.10:41203", nil, nil)
	var statusBody struct {
		Required bool `json:"required"`
	}
	if err := json.Unmarshal(status.Body.Bytes(), &statusBody); err != nil || statusBody.Required {
		t.Fatalf("setup 完成后 required=(%v, %v)，期望 false", statusBody.Required, err)
	}
}

func TestSetup已有管理员时隐藏端点为NotFound(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	response := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/setup",
		`{"username":"Another Admin","password":"`+httpTestPassword+`"}`,
		"127.0.0.1:41301",
		nil,
		nil,
	)
	assertAPIError(t, response, http.StatusNotFound, "not_found")
}

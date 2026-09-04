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

func TestSetup默认拒绝同机反向代理转发的公网Host(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	response := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/setup",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		"127.0.0.1:41102",
		nil,
		map[string]string{"Host": "reposentinel.example"},
	)
	assertAPIError(t, response, http.StatusForbidden, "forbidden")
}

func TestSetup默认允许直连LoopbackHost(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	response := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/setup",
		`{"username":"Repo Admin","password":"`+httpTestPassword+`"}`,
		"127.0.0.1:41103",
		nil,
		map[string]string{"Host": "127.0.0.1:8080"},
	)
	if response.Code != http.StatusCreated {
		t.Fatalf("本机直连 setup 状态=%d，响应=%s", response.Code, response.Body.String())
	}
}

func TestLoopbackHost识别本机Host并拒绝公网或畸形Host(t *testing.T) {
	tests := []struct {
		name string
		host string
		want bool
	}{
		{name: "localhost", host: "localhost", want: true},
		{name: "localhost trailing dot", host: "LOCALHOST.", want: true},
		{name: "IPv4 with port", host: "127.0.0.1:8080", want: true},
		{name: "bracketed IPv6", host: "[::1]", want: true},
		{name: "bracketed IPv6 with port", host: "[::1]:8080", want: true},
		{name: "public hostname", host: "reposentinel.example", want: false},
		{name: "malformed port", host: "localhost:not-a-port", want: false},
		{name: "out of range port", host: "localhost:70000", want: false},
		{name: "empty", host: "", want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := isLoopbackHost(test.host); got != test.want {
				t.Fatalf("isLoopbackHost(%q)=%v，期望 %v", test.host, got, test.want)
			}
		})
	}
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
		map[string]string{"Host": "reposentinel.example"},
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

func TestSetupRejectsBlankCredentials(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	// 空用户名
	respUser := fixture.request(
		t, http.MethodPost, "/api/v1/setup",
		`{"username":"   ","password":"`+httpTestPassword+`"}`,
		"127.0.0.1:41110", nil, map[string]string{"Host": "127.0.0.1:8080"},
	)
	assertAPIError(t, respUser, http.StatusBadRequest, "validation_failed")

	// 空密码
	respPass := fixture.request(
		t, http.MethodPost, "/api/v1/setup",
		`{"username":"admin","password":"   "}`,
		"127.0.0.1:41111", nil, map[string]string{"Host": "127.0.0.1:8080"},
	)
	assertAPIError(t, respPass, http.StatusBadRequest, "validation_failed")
}

func TestSetupPreservesPasswordWhitespace(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	password := " 管理员初始密码一二三四五六 "

	created := fixture.request(
		t, http.MethodPost, "/api/v1/setup",
		`{"username":"Repo Admin","password":"`+password+`"}`,
		"127.0.0.1:41112", nil, map[string]string{"Host": "127.0.0.1:8080"},
	)
	if created.Code != http.StatusCreated {
		t.Fatalf("带首尾空格的合法密码不应阻止 setup，状态=%d，响应=%s", created.Code, created.Body.String())
	}

	login := fixture.request(
		t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+password+`"}`,
		"127.0.0.1:41113", nil, nil,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("setup 必须保留密码首尾空格以便原值登录，状态=%d，响应=%s", login.Code, login.Body.String())
	}
}

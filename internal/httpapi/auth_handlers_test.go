package httpapi

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
)

func Test登录错误统一且第六次尝试被限流(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	for attempt := 1; attempt <= 5; attempt++ {
		response := fixture.request(
			t,
			http.MethodPost,
			"/api/v1/auth/login",
			`{"username":"Repo Admin","password":"错误管理员密码一二三四五六"}`,
			"198.51.100.10:42001",
			nil,
			nil,
		)
		assertAPIError(t, response, http.StatusUnauthorized, "invalid_credentials")
	}
	sixth := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"username":" repo admin ","password":"错误管理员密码一二三四五六"}`,
		"198.51.100.10:42002",
		nil,
		nil,
	)
	assertAPIError(t, sixth, http.StatusTooManyRequests, "rate_limited")
}

func Test登录设置安全Cookie并返回当前Session(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	assertAuthCookies(t, cookies)

	response := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:42101", cookies, nil)
	if response.Code != http.StatusOK {
		t.Fatalf("读取当前 Session 状态=%d，响应=%s", response.Code, response.Body.String())
	}
	if cacheControl := response.Header().Get("Cache-Control"); cacheControl != "no-store" {
		t.Fatalf("Session 响应 Cache-Control=%q，期望 no-store", cacheControl)
	}
}

func Test认证写操作缺失CSRF失败且成功退出清理Cookie(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	missing := fixture.request(t, http.MethodPost, "/api/v1/auth/logout", `{}`, "127.0.0.1:42201", cookies, nil)
	assertAPIError(t, missing, http.StatusForbidden, "csrf_failed")
	stillActive := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:42202", cookies, nil)
	if stillActive.Code != http.StatusOK {
		t.Fatalf("CSRF 失败后 Session 不应被撤销: %s", stillActive.Body.String())
	}

	csrfCookie := cookieByName(t, cookies, CSRFCookieName)
	logout := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/logout",
		`{}`,
		"127.0.0.1:42203",
		cookies,
		map[string]string{CSRFHeaderName: csrfCookie.Value},
	)
	if logout.Code != http.StatusOK {
		t.Fatalf("退出状态=%d，响应=%s", logout.Code, logout.Body.String())
	}
	cleared := logout.Result().Cookies()
	if cookieByName(t, cleared, SessionCookieName).MaxAge >= 0 || cookieByName(t, cleared, CSRFCookieName).MaxAge >= 0 {
		t.Fatal("退出必须清理 Session 与 CSRF Cookie")
	}
	after := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:42204", cookies, nil)
	assertAPIError(t, after, http.StatusUnauthorized, "unauthorized")
}

func Test修改密码只保留当前Session(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	firstCookies := fixture.login(t, httpTestPassword)
	currentCookies := fixture.login(t, httpTestPassword)
	csrfCookie := cookieByName(t, currentCookies, CSRFCookieName)

	changed := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/password",
		`{"current_password":"`+httpTestPassword+`","new_password":"`+httpChangedPassword+`"}`,
		"127.0.0.1:42301",
		currentCookies,
		map[string]string{CSRFHeaderName: csrfCookie.Value},
	)
	if changed.Code != http.StatusOK {
		t.Fatalf("修改密码状态=%d，响应=%s", changed.Code, changed.Body.String())
	}
	first := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:42302", firstCookies, nil)
	assertAPIError(t, first, http.StatusUnauthorized, "unauthorized")
	current := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:42303", currentCookies, nil)
	if current.Code != http.StatusOK {
		t.Fatalf("当前 Session 应保留，状态=%d，响应=%s", current.Code, current.Body.String())
	}
	if _, err := fixture.adminService.Authenticate(t.Context(), "Repo Admin", httpTestPassword); !errors.Is(err, auth.ErrInvalidCredentials) {
		t.Fatalf("旧密码认证错误=%v，期望 invalid_credentials", err)
	}
	if _, err := fixture.adminService.Authenticate(t.Context(), "Repo Admin", httpChangedPassword); err != nil {
		t.Fatalf("新密码应可认证: %v", err)
	}
}

func assertAuthCookies(t *testing.T, cookies []*http.Cookie) {
	t.Helper()
	session := cookieByName(t, cookies, SessionCookieName)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	if !session.HttpOnly || !session.Secure || session.SameSite != http.SameSiteLaxMode || session.Path != "/" || session.MaxAge <= 0 {
		t.Fatalf("Session Cookie 属性不安全: %+v", session)
	}
	if csrf.HttpOnly || !csrf.Secure || csrf.SameSite != http.SameSiteLaxMode || csrf.Path != "/" || csrf.MaxAge <= 0 {
		t.Fatalf("CSRF Cookie 属性不符合双提交要求: %+v", csrf)
	}
}

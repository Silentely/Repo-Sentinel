package httpapi

import (
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

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
			`{"username":"unknown-`+string(rune('a'+attempt-1))+`","password":"错误管理员密码一二三四五六"}`,
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
		`{"username":"unknown-z","password":"错误管理员密码一二三四五六"}`,
		"198.51.100.10:42002",
		nil,
		nil,
	)
	assertAPIError(t, sixth, http.StatusTooManyRequests, "rate_limited")
}

// TestLoginFailureLogsUsername 凭据错误日志必须带 username 与 remote_ip（不含密码）：
// 审计暴力尝试需要账号维度；密码一旦入日志即泄露，绝不能记。
func TestLoginFailureLogsUsername(t *testing.T) {
	logBuffer := &lockedBuffer{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		logger: slog.New(slog.NewJSONHandler(logBuffer, nil)),
	})
	fixture.bootstrapAdmin(t)

	fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"username":"attacker-bot","password":"错误管理员密码一二三四五六"}`,
		"198.51.100.66:42003",
		nil,
		nil,
	)

	logs := logBuffer.String()
	for _, want := range []string{`"msg":"login failed"`, `"username":"attacker-bot"`, `"remote_ip":"198.51.100.66"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("登录失败日志应包含 %s，实际: %s", want, logs)
		}
	}
	if strings.Contains(logs, "错误管理员密码") {
		t.Fatal("登录失败日志绝不能包含密码")
	}
}

// TestCSRFFailureLogsRequestContext CSRF 校验失败必须留痕来源信息（写请求被拒常是安全事件），
// 日志只含 request_id/remote_ip 与路径，不暴露令牌内容。
func TestCSRFFailureLogsRequestContext(t *testing.T) {
	logBuffer := &lockedBuffer{}
	fixture := newHTTPTestFixture(t, httpTestOptions{
		logger: slog.New(slog.NewJSONHandler(logBuffer, nil)),
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 带 Session Cookie 但缺 CSRF Cookie 的写请求 → csrf_failed。
	fixture.request(t, http.MethodPost, "/api/v1/auth/logout", "", "127.0.0.1:42201", cookies, nil)

	logs := logBuffer.String()
	for _, want := range []string{`"msg":"csrf validation failed"`, `"remote_ip":"127.0.0.1"`, `"error_code":"csrf_failed"`, `"path":"/api/v1/auth/logout"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("CSRF 失败日志应包含 %s，实际: %s", want, logs)
		}
	}
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

func TestChangePasswordRejectsBlankPasswords(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrfCookie := cookieByName(t, cookies, CSRFCookieName)

	// 空当前密码
	blankCurrent := fixture.request(
		t, http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"   ","new_password":"valid-new-password"}`,
		"127.0.0.1:42310", cookies, map[string]string{CSRFHeaderName: csrfCookie.Value},
	)
	assertAPIError(t, blankCurrent, http.StatusBadRequest, "validation_failed")

	// 空新密码
	blankNew := fixture.request(
		t, http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"`+httpTestPassword+`","new_password":"   "}`,
		"127.0.0.1:42311", cookies, map[string]string{CSRFHeaderName: csrfCookie.Value},
	)
	assertAPIError(t, blankNew, http.StatusBadRequest, "validation_failed")
}

func TestChangePasswordPreservesPasswordWhitespace(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	currentCookies := fixture.login(t, httpTestPassword)
	csrfCookie := cookieByName(t, currentCookies, CSRFCookieName)
	newPassword := " 管理员更新密码一二三四五六 "

	changed := fixture.request(
		t, http.MethodPost, "/api/v1/auth/password",
		`{"current_password":"`+httpTestPassword+`","new_password":"`+newPassword+`"}`,
		"127.0.0.1:42312", currentCookies, map[string]string{CSRFHeaderName: csrfCookie.Value},
	)
	if changed.Code != http.StatusOK {
		t.Fatalf("带首尾空格的新密码不应被截断，改密状态=%d，响应=%s", changed.Code, changed.Body.String())
	}

	login := fixture.request(
		t, http.MethodPost, "/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+newPassword+`"}`,
		"127.0.0.1:42313", nil, nil,
	)
	if login.Code != http.StatusOK {
		t.Fatalf("改密后必须使用原始新密码登录，状态=%d，响应=%s", login.Code, login.Body.String())
	}
}

func TestLoginAccountFailureDelayAndConcurrency(t *testing.T) {
	t.Run("账号认证失败记录连续失败并在后续引入延迟", func(t *testing.T) {
		fixture := newHTTPTestFixture(t, httpTestOptions{})
		fixture.bootstrapAdmin(t)

		// 连续错 3 次
		for i := 0; i < 3; i++ {
			ip := fmt.Sprintf("198.51.100.%d", i+1)
			res := fixture.request(
				t,
				http.MethodPost,
				"/api/v1/auth/login",
				`{"username":"Repo Admin","password":"wrong-password"}`,
				ip,
				nil,
				nil,
			)
			assertAPIError(t, res, http.StatusUnauthorized, "invalid_credentials")
		}

		// 检查 limiter 对 admin 的延迟惩罚应生效
		delay := fixture.loginLimiter.DelayFor("Repo Admin")
		if delay < 500*time.Millisecond {
			t.Fatalf("连续失败3次后延迟=%v, want >= 500ms", delay)
		}

		// 正确密码登录成功
		cookies := fixture.login(t, httpTestPassword)
		assertAuthCookies(t, cookies)

		// 成功后延迟清零
		if delay := fixture.loginLimiter.DelayFor("Repo Admin"); delay != 0 {
			t.Fatalf("登录成功后延迟=%v, want 0", delay)
		}
	})
}

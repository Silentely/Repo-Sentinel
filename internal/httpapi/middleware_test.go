package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

const (
	httpTestPassword    = "管理员初始密码一二三四五六"
	httpChangedPassword = "管理员更新密码一二三四五六"
)

func Test中间件提供JSON请求标识安全头与缓存保护(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	live := fixture.request(t, http.MethodGet, "/health/live", "", "127.0.0.1:40001", nil, nil)
	if live.Code != http.StatusOK {
		t.Fatalf("live 状态码=%d，期望 200；响应=%s", live.Code, live.Body.String())
	}
	if requestID := live.Header().Get("X-Request-ID"); requestID == "" {
		t.Fatal("响应缺少 X-Request-ID")
	}
	if contentType := live.Header().Get("Content-Type"); !strings.HasPrefix(contentType, "application/json") {
		t.Fatalf("Content-Type=%q，期望 application/json", contentType)
	}
	assertSecurityHeaders(t, live.Header())
	if value := live.Header().Get("Access-Control-Allow-Origin"); value != "" {
		t.Fatalf("管理 API 不应默认启用宽泛 CORS，实际=%q", value)
	}

	unauthorized := fixture.request(t, http.MethodGet, "/api/v1/auth/session", "", "127.0.0.1:40002", nil, nil)
	assertAPIError(t, unauthorized, http.StatusUnauthorized, "unauthorized")
	if cacheControl := unauthorized.Header().Get("Cache-Control"); !strings.Contains(cacheControl, "no-store") {
		t.Fatalf("401 Cache-Control=%q，期望 no-store", cacheControl)
	}
}

func TestJSON解码拒绝未知字段非JSON与超过一MiB请求体(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})

	unknown := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"username":"Repo Admin","password":"wrong","unexpected":true}`,
		"127.0.0.1:40101",
		nil,
		nil,
	)
	assertAPIError(t, unknown, http.StatusBadRequest, "validation_failed")

	wrongContentType := fixture.requestWithContentType(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"username":"Repo Admin","password":"wrong"}`,
		"text/plain",
		"127.0.0.1:40102",
		nil,
		nil,
	)
	assertAPIError(t, wrongContentType, http.StatusUnsupportedMediaType, "validation_failed")

	oversizedBody := `{"username":"Repo Admin","password":"` + strings.Repeat("a", (1<<20)+1) + `"}`
	oversized := fixture.request(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		oversizedBody,
		"127.0.0.1:40103",
		nil,
		nil,
	)
	assertAPIError(t, oversized, http.StatusRequestEntityTooLarge, "validation_failed")
}

func TestPanic恢复返回内部错误且不泄漏堆栈或Panic内容(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		ready: ReadyCheckFunc(func(context.Context) error {
			panic("sensitive-database-password")
		}),
	})

	response := fixture.request(t, http.MethodGet, "/health/ready", "", "127.0.0.1:40201", nil, nil)
	assertAPIError(t, response, http.StatusInternalServerError, "internal_error")
	body := response.Body.String()
	if strings.Contains(body, "sensitive-database-password") || strings.Contains(body, "goroutine") {
		t.Fatalf("panic 响应泄漏内部信息: %s", body)
	}
	if response.Header().Get("X-Request-ID") == "" {
		t.Fatal("panic 响应仍必须包含 X-Request-ID")
	}
}

type httpTestOptions struct {
	allowRemote   bool
	publicBaseURL string
	ready         ReadyChecker
	frontend      fs.FS
	keyRing       *cryptox.KeyRing
	// envAppID > 0 时模拟环境变量已设置 App ID（管理台锁定）。
	envAppID int64
}

type httpTestFixture struct {
	handler        http.Handler
	store          store.Store
	adminService   *auth.AdminService
	sessionService *auth.SessionService
	clock          *httpTestClock
	githubRuntime  *githubx.RuntimeConfig
}

func newHTTPTestFixture(t *testing.T, options httpTestOptions) *httpTestFixture {
	t.Helper()
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(temporaryDir, "httpapi.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开 HTTP 测试 Store 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := opened.Close(); err != nil {
			t.Errorf("关闭 HTTP 测试 Store 失败: %v", err)
		}
	})

	publicBaseURL := options.publicBaseURL
	if publicBaseURL == "" {
		publicBaseURL = "https://reposentinel.example"
	}
	clock := newHTTPTestClock(time.Date(2026, 7, 27, 20, 0, 0, 0, time.UTC))
	adminService := auth.NewAdminService(opened, auth.NewPasswordHasher())
	sessionService := auth.NewSessionService(opened, clock, rand.Reader, time.Hour)
	ready := options.ready
	if ready == nil {
		ready = ReadyCheckFunc(func(context.Context) error { return nil })
	}
	ghRuntime := &githubx.RuntimeConfig{
		PublicBaseURL:       publicBaseURL,
		PublicBaseURLSource: "env",
		WebhookSecretSource: "unset",
		AppIDSource:         "unset",
		ClientIDSource:      "unset",
		PrivateKeySource:    "unset",
		ExternalPATSource:   "unset",
	}
	cfg := config.Config{
		HTTP:     config.HTTPConfig{PublicBaseURL: publicBaseURL},
		Database: config.DatabaseConfig{Driver: "sqlite"},
		Admin:    config.AdminBootstrapConfig{SessionTTL: time.Hour},
		Setup:    config.SetupConfig{AllowRemote: options.allowRemote},
		UpdateCheck: config.UpdateCheckConfig{
			Enabled: false, // 单测默认不联网
		},
	}
	if options.envAppID > 0 {
		cfg.GitHub.AppID = options.envAppID
		ghRuntime.AppID = options.envAppID
		ghRuntime.AppIDSource = "env"
	}
	dependencies := Dependencies{
		Config:         cfg,
		Store:          opened,
		AdminStore:     opened.Admins(),
		AdminService:   adminService,
		SessionService: sessionService,
		CSRF:           auth.NewCSRFTokens(rand.Reader),
		LoginLimiter:   auth.NewLoginLimiter(clock),
		BuildInfo: buildinfo.Info{
			Version:      "0.1.0",
			GitSHA:       "0123456789abcdef",
			GitBranch:    "feature/reposentinel-mvp",
			BuildTime:    "2026-07-27T12:00:00Z",
			BuildChannel: "test",
			GoVersion:    "go1.26.4",
		},
		Ready:         ready,
		Logger:        slog.New(slog.NewJSONHandler(io.Discard, nil)),
		SchemaVersion: "202607270001",
		Frontend:      options.frontend,
		KeyRing:       options.keyRing,
		GitHubRuntime: ghRuntime,
	}
	return &httpTestFixture{
		handler:        New(dependencies),
		store:          opened,
		adminService:   adminService,
		sessionService: sessionService,
		clock:          clock,
		githubRuntime:  ghRuntime,
	}
}

func (f *httpTestFixture) request(
	t *testing.T,
	method, path, body, remoteAddr string,
	cookies []*http.Cookie,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	contentType := ""
	if body != "" {
		contentType = "application/json"
	}
	return f.requestWithContentType(t, method, path, body, contentType, remoteAddr, cookies, headers)
}

func (f *httpTestFixture) requestWithContentType(
	t *testing.T,
	method, path, body, contentType, remoteAddr string,
	cookies []*http.Cookie,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	request := httptest.NewRequest(method, path, strings.NewReader(body))
	request.RemoteAddr = remoteAddr
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	for _, cookie := range cookies {
		request.AddCookie(cookie)
	}
	for name, value := range headers {
		if name == "Host" {
			request.Host = value
			continue
		}
		request.Header.Set(name, value)
	}
	response := httptest.NewRecorder()
	f.handler.ServeHTTP(response, request)
	return response
}

func (f *httpTestFixture) bootstrapAdmin(t *testing.T) auth.Admin {
	t.Helper()
	admin, err := f.adminService.BootstrapAdmin(t.Context(), "Repo Admin", httpTestPassword)
	if err != nil {
		t.Fatalf("准备 HTTP 测试管理员失败: %v", err)
	}
	return admin
}

func (f *httpTestFixture) login(t *testing.T, password string) []*http.Cookie {
	t.Helper()
	response := f.request(
		t,
		http.MethodPost,
		"/api/v1/auth/login",
		`{"username":"Repo Admin","password":"`+password+`"}`,
		"127.0.0.1:40501",
		nil,
		nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("登录失败: 状态=%d, 响应=%s", response.Code, response.Body.String())
	}
	return response.Result().Cookies()
}

func assertAPIError(t *testing.T, response *httptest.ResponseRecorder, status int, errorCode string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("状态码=%d，期望 %d；响应=%s", response.Code, status, response.Body.String())
	}
	var body struct {
		ErrorCode string         `json:"error_code"`
		Message   string         `json:"message"`
		Details   map[string]any `json:"details"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("错误响应不是合法 JSON: %v；响应=%s", err, response.Body.String())
	}
	if body.ErrorCode != errorCode || strings.TrimSpace(body.Message) == "" {
		t.Fatalf("错误响应=(%q, %q)，期望 error_code=%q 且 message 非空", body.ErrorCode, body.Message, errorCode)
	}
}

func assertSecurityHeaders(t *testing.T, header http.Header) {
	t.Helper()
	if header.Get("X-Content-Type-Options") != "nosniff" {
		t.Fatal("缺少 X-Content-Type-Options=nosniff")
	}
	if header.Get("Referrer-Policy") != "no-referrer" {
		t.Fatal("缺少 Referrer-Policy=no-referrer")
	}
	if !strings.Contains(header.Get("Content-Security-Policy"), "frame-ancestors 'none'") {
		t.Fatal("CSP 缺少 frame-ancestors 'none'")
	}
	if strings.Contains(header.Get("Content-Security-Policy"), "'unsafe-inline'") {
		t.Fatal("CSP 不应为计划中的外部样式资源开放 unsafe-inline")
	}
	if !strings.Contains(header.Get("Permissions-Policy"), "camera=()") {
		t.Fatal("Permissions-Policy 未禁用无关传感器")
	}
}

func cookieByName(t *testing.T, cookies []*http.Cookie, name string) *http.Cookie {
	t.Helper()
	for _, cookie := range cookies {
		if cookie.Name == name {
			return cookie
		}
	}
	t.Fatalf("未找到 Cookie %s", name)
	return nil
}

type httpTestClock struct {
	mu  sync.RWMutex
	now time.Time
}

func newHTTPTestClock(now time.Time) *httpTestClock {
	return &httpTestClock{now: now.UTC()}
}

func (c *httpTestClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *httpTestClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

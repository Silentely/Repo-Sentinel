package httpapi

import (
	"bytes"
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

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
)

const (
	httpTestPassword    = "管理员初始密码一二三四五六"
	httpChangedPassword = "管理员更新密码一二三四五六"
)

// TestAccessLogIncludesUserAgent 访问日志（debug 级）必须携带 User-Agent，
// 便于区分浏览器、Agent 客户端与爬虫流量。
func TestAccessLogIncludesUserAgent(t *testing.T) {
	var logBuffer bytes.Buffer
	s := &server{dependencies: Dependencies{Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))}}
	handler := s.accessLogMiddleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 RepoSentinel-TestBot")
	handler.ServeHTTP(httptest.NewRecorder(), req)
	if !strings.Contains(logBuffer.String(), "RepoSentinel-TestBot") {
		t.Fatalf("access log 应包含 user_agent，实际日志: %s", logBuffer.String())
	}
}

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

// TestPanic恢复返回内部错误且不泄漏堆栈或Panic内容
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

// TestPanic日志携带Panic值与堆栈 panic 恢复日志必须同时携带 panic 值与调用堆栈，
// 否则事故现场只剩 error_code=internal_error，无法定位 panic 源头。
func TestPanic日志携带Panic值与堆栈(t *testing.T) {
	var logBuffer bytes.Buffer
	s := &server{dependencies: Dependencies{Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))}}
	handler := s.recoveryMiddleware(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom: nil pointer dereference")
	}))
	req := httptest.NewRequest(http.MethodGet, "/api/v1/dashboard", nil)
	handler.ServeHTTP(httptest.NewRecorder(), req)

	logs := logBuffer.String()
	if !strings.Contains(logs, "boom: nil pointer dereference") {
		t.Fatalf("panic 日志应携带 panic 值，实际日志: %s", logs)
	}
	if !strings.Contains(logs, "recoveryMiddleware") {
		t.Fatalf("panic 日志应携带调用堆栈（含 recoveryMiddleware 帧），实际日志: %s", logs)
	}
	if !strings.Contains(logs, "error_code=internal_error") && !strings.Contains(logs, `"error_code":"internal_error"`) {
		t.Fatalf("panic 日志应保留 error_code=internal_error，实际日志: %s", logs)
	}
}

type httpTestOptions struct {
	allowRemote   bool
	publicBaseURL string
	ready         ReadyChecker
	frontend      fs.FS
	keyRing       *cryptox.KeyRing
	// logger 注入日志断言；nil 时默认丢弃输出。
	logger *slog.Logger
	// metricsEnabled 启用 /metrics 路由（指标端点测试用）。
	metricsEnabled bool
	// oauthClientID / oauthClientSecret 装配到 Config.OAuth，用于 Agent Bearer 认证测试。
	oauthClientID     string
	oauthClientSecret string
	// envAppID > 0 时模拟环境变量已设置 App ID（管理台锁定）。
	envAppID int64
	// updateChecker 装配到 Dependencies.UpdateChecker；nil 时 handler 回退临时检查器。
	// 注入后可断言共享 Checker 的状态不被请求改写（并发数据竞争回归用）。
	updateChecker *updatecheck.Checker
	// decorateStore 包装装配到 Dependencies.Store 的实现；AdminStore、认证与会话
	// 仍持有原始 Store，用于"只让指定子存储故障"的错误分支注入。
	decorateStore func(store.Store) store.Store
	// aiRuntime / aiClient 装配到 Dependencies，供 AI 配置 API 测试。
	aiRuntime *ai.RuntimeConfig
	aiClient  *ai.Client
	// aiConfig 装配到 Dependencies.Config.AI，供 PUT 热更新路径使用；
	// 需与 aiRuntime 同源（默认值一致），避免 RuntimeFromEnv 把零值误判为 env 锁定。
	aiConfig config.AIConfig
	// starPoller 装配到 Dependencies.StarredPoller，供 star release 追踪 API 测试。
	starPoller *syncx.StarredReleasePoller
}

type httpTestFixture struct {
	handler        http.Handler
	store          store.Store
	adminService   *auth.AdminService
	sessionService *auth.SessionService
	loginLimiter   *auth.LoginLimiter
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
		HTTP:        config.HTTPConfig{PublicBaseURL: publicBaseURL},
		Database:    config.DatabaseConfig{Driver: "sqlite"},
		Admin:       config.AdminBootstrapConfig{SessionTTL: time.Hour},
		Setup:       config.SetupConfig{AllowRemote: options.allowRemote},
		Metrics:     config.MetricsConfig{Enabled: options.metricsEnabled},
		UpdateCheck: config.UpdateCheckConfig{Enabled: false}, // 单测默认不联网
		AI:          options.aiConfig,

		OAuth: config.OAuthConfig{
			ClientID:     options.oauthClientID,
			ClientSecret: config.NewSecret(options.oauthClientSecret),
		},
	}
	if options.envAppID > 0 {
		cfg.GitHub.AppID = options.envAppID
		ghRuntime.AppID = options.envAppID
		ghRuntime.AppIDSource = "env"
	}
	logger := options.logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(io.Discard, nil))
	}
	// decorateStore 仅包装 handler 持有的 Store；登录、会话与 CSRF 依赖的
	// AdminService/SessionService 继续使用原始 opened，保证鉴权路径不受注入故障影响。
	handlerStore := opened
	if options.decorateStore != nil {
		handlerStore = options.decorateStore(opened)
	}
	dependencies := Dependencies{
		Config:         cfg,
		Store:          handlerStore,
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
		Logger:        logger,
		SchemaVersion: "202607270001",
		Frontend:      options.frontend,
		KeyRing:       options.keyRing,
		GitHubRuntime: ghRuntime,
		UpdateChecker: options.updateChecker,
		AI:            options.aiClient,
		AIRuntime:     options.aiRuntime,
		StarredPoller: options.starPoller,
	}
	return &httpTestFixture{
		handler:        New(dependencies),
		store:          opened,
		adminService:   adminService,
		sessionService: sessionService,
		loginLimiter:   dependencies.LoginLimiter,
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
	if !strings.Contains(header.Get("Content-Security-Policy"), "form-action 'self'") {
		t.Fatal("CSP 缺少 form-action 'self'")
	}
	if strings.Contains(header.Get("Content-Security-Policy"), "'unsafe-inline'") {
		t.Fatal("CSP 不应为计划中的外部样式资源开放 unsafe-inline")
	}
	if !strings.Contains(header.Get("Permissions-Policy"), "camera=()") {
		t.Fatal("Permissions-Policy 未禁用无关传感器")
	}
	if header.Get("Strict-Transport-Security") == "" {
		t.Fatal("HTTPS 部署应下发 Strict-Transport-Security")
	}
	if !strings.Contains(header.Get("Content-Security-Policy"), "upgrade-insecure-requests") {
		t.Fatal("HTTPS 部署 CSP 应含 upgrade-insecure-requests")
	}
}

// TestPlainHTTPNoHSTS 明文部署（PublicBaseURL 非 https）不下发 HSTS：
// 否则浏览器会强制升级后续请求，把纯 HTTP 自托管部署锁死。
func TestPlainHTTPNoHSTS(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{publicBaseURL: "http://reposentinel.local"})
	resp := fixture.request(t, http.MethodGet, "/health/live", "", "127.0.0.1:42501", nil, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("live status=%d", resp.Code)
	}
	if got := resp.Header().Get("Strict-Transport-Security"); got != "" {
		t.Fatalf("明文部署不应下发 HSTS，实际: %q", got)
	}
	if strings.Contains(resp.Header().Get("Content-Security-Policy"), "upgrade-insecure-requests") {
		t.Fatal("明文部署 CSP 不应含 upgrade-insecure-requests")
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

func TestRealIPMiddlewareWithTrustedProxies(t *testing.T) {
	captureRemoteIP := func(s *server) (http.Handler, *string) {
		var captured string
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			captured = remoteIPFromContext(r.Context())
			w.WriteHeader(http.StatusOK)
		})
		return s.realIPMiddleware(inner), &captured
	}

	t.Run("默认无受信任代理时忽略代理头", func(t *testing.T) {
		s := &server{dependencies: Dependencies{}}
		handler, captured := captureRemoteIP(s)

		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.195")
		req.Header.Set("X-Real-IP", "203.0.113.196")

		handler.ServeHTTP(httptest.NewRecorder(), req)
		if *captured != "127.0.0.1" {
			t.Fatalf("remoteIP=%q, want 127.0.0.1", *captured)
		}
	})

	t.Run("直接对端不在受信任代理内时忽略代理头", func(t *testing.T) {
		s := &server{dependencies: Dependencies{
			Config: config.Config{
				HTTP: config.HTTPConfig{
					TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8"},
				},
			},
		}}
		handler, captured := captureRemoteIP(s)

		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		req.RemoteAddr = "198.51.100.2:12345"
		req.Header.Set("X-Forwarded-For", "203.0.113.195")

		handler.ServeHTTP(httptest.NewRecorder(), req)
		if *captured != "198.51.100.2" {
			t.Fatalf("remoteIP=%q, want 198.51.100.2", *captured)
		}
	})

	t.Run("直接对端受信任时从X-Forwarded-For反向提取最右侧不可信IP", func(t *testing.T) {
		s := &server{dependencies: Dependencies{
			Config: config.Config{
				HTTP: config.HTTPConfig{
					TrustedProxies: []string{"127.0.0.1", "10.0.0.0/8"},
				},
			},
		}}
		handler, captured := captureRemoteIP(s)

		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		// 客户端 203.0.113.50 经过内部代理 10.0.1.2 转发到本机 127.0.0.1
		req.Header.Set("X-Forwarded-For", "203.0.113.50, 10.0.1.2")

		handler.ServeHTTP(httptest.NewRecorder(), req)
		if *captured != "203.0.113.50" {
			t.Fatalf("remoteIP=%q, want 203.0.113.50", *captured)
		}
	})

	t.Run("直接对端受信任且仅提供X-Real-IP时生效", func(t *testing.T) {
		s := &server{dependencies: Dependencies{
			Config: config.Config{
				HTTP: config.HTTPConfig{
					TrustedProxies: []string{"127.0.0.1"},
				},
			},
		}}
		handler, captured := captureRemoteIP(s)

		req := httptest.NewRequest(http.MethodGet, "/health/live", nil)
		req.RemoteAddr = "127.0.0.1:12345"
		req.Header.Set("X-Real-IP", "198.51.100.77")

		handler.ServeHTTP(httptest.NewRecorder(), req)
		if *captured != "198.51.100.77" {
			t.Fatalf("remoteIP=%q, want 198.51.100.77", *captured)
		}
	})
}

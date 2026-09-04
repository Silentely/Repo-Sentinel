package httpapi

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
	"github.com/go-chi/chi/v5"
)

const (
	// SessionCookieName 是服务端 Session 原始令牌的固定 Cookie 名。
	SessionCookieName = "reposentinel_session"
	// CSRFCookieName 是双提交 CSRF 原始令牌的固定 Cookie 名。
	CSRFCookieName = "reposentinel_csrf"
	// CSRFHeaderName 是写请求携带双提交令牌的固定 Header 名。
	CSRFHeaderName = "X-CSRF-Token"
	// webhookProcessConcurrency 限制 webhook 后台处理并发数：突发事件（如仓库批量推送）
	// 不会无限起 goroutine 同时写库/调 GitHub API，超出部分排队等待而非丢弃。
	webhookProcessConcurrency = 32
)

// ReadyChecker 报告迁移、数据库与核心依赖是否已就绪。
type ReadyChecker interface {
	Ready(context.Context) error
}

// ReadyCheckFunc 将普通函数适配为 ReadyChecker。
type ReadyCheckFunc func(context.Context) error

// Ready 执行就绪检查。
func (f ReadyCheckFunc) Ready(ctx context.Context) error {
	return f(ctx)
}

// Dependencies 是 HTTP 管理面的显式最小依赖集合。
type Dependencies struct {
	Config         config.Config
	Store          store.Store
	AdminStore     store.AdminStore
	AdminService   *auth.AdminService
	SessionService *auth.SessionService
	CSRF           auth.CSRFTokens
	LoginLimiter   *auth.LoginLimiter
	BuildInfo      buildinfo.Info
	Ready          ReadyChecker
	Logger         *slog.Logger
	SchemaVersion  string
	Frontend       fs.FS
	KeyRing        *cryptox.KeyRing
	Aggregator     *rules.Aggregator
	Reconciler     *syncx.Reconciler
	// UpdateChecker 可选；关于页远程版本检查。
	UpdateChecker *updatecheck.Checker
	// GitHubRuntime 可选；管理台可编辑的 GitHub 配置（env 优先，DB 补缺）。
	GitHubRuntime *githubx.RuntimeConfig
	// Background 用于 Webhook 异步规范化；关闭时由 App 取消。
	Background context.Context
	// AI 可选；默认 rules.Engine 的安全告警分诊客户端（webhooksvc 透传）。
	AI *ai.Client
	// AIRuntime 可选；管理台可编辑的 AI 配置（env 优先，DB 补缺）。
	AIRuntime *ai.RuntimeConfig
	// StarredPoller 可选；star 仓库 release 追踪轮询（配置保存/立即同步触发）。
	StarredPoller *syncx.StarredReleasePoller
}

type server struct {
	dependencies   Dependencies
	secureCookies  bool
	trustedProxies []*net.IPNet
	// webhookSvc 承担 Webhook 后台管线（规范化 → 通知 → 状态机），装配一次复用。
	webhookSvc *webhooksvc.Service
	// reconcileAllRunning 防止全量对账并发触发（对账会大量调用 GitHub API 并写库）。
	reconcileAllRunning atomic.Bool
	// webhookSem 控制 webhook 后台处理并发；容量见 webhookProcessConcurrency。
	webhookSem chan struct{}
	// loginSem 控制 Argon2id 认证并发计算上限，防止 CPU 耗尽。
	loginSem    chan struct{}
	totpTickets *auth.TOTPTicketManager
}

// safeGo 以后台 goroutine 执行 fn；panic 只记录日志，不拖垮整个进程。
// recoveryMiddleware 只能护住同步请求路径，后台任务必须单独设防。
func (s *server) safeGo(name string, fn func()) {
	go func() {
		defer func() {
			if rec := recover(); rec != nil {
				s.dependencies.Logger.Error(
					"background task panic",
					"task", name,
					"error_code", "background_panic",
					"panic", fmt.Sprint(rec),
				)
			}
		}()
		fn()
	}()
}

func (s *server) getTOTPTickets() *auth.TOTPTicketManager {
	if s == nil {
		return auth.NewTOTPTicketManager(3 * time.Minute)
	}
	if s.totpTickets != nil {
		return s.totpTickets
	}
	s.totpTickets = auth.NewTOTPTicketManager(3 * time.Minute)
	return s.totpTickets
}

func (s *server) getTrustedProxies() []*net.IPNet {
	if s == nil {
		return nil
	}
	if s.trustedProxies != nil {
		return s.trustedProxies
	}
	if len(s.dependencies.Config.HTTP.TrustedProxies) > 0 {
		return parseTrustedSubnets(s.dependencies.Config.HTTP.TrustedProxies)
	}
	return nil
}

// acquireWebhookSlot 领取 webhook 处理槽位；槽位满时阻塞等待（事件不丢弃）。
// ctx 为 nil 时其 Done 通道永不就绪，等价于始终等待；关闭期间返回 false 不再排队。
// webhookSem 未装配（直接构造的测试 server）时不限制并发。
func (s *server) acquireWebhookSlot(ctx context.Context) bool {
	if s.webhookSem == nil {
		return true
	}
	select {
	case s.webhookSem <- struct{}{}:
		return true
	case <-ctx.Done():
		return false
	}
}

// releaseWebhookSlot 归还 webhook 处理槽位，与 acquireWebhookSlot 配对使用。
func (s *server) releaseWebhookSlot() {
	<-s.webhookSem
}

// processWebhookAsync 后台执行 webhook 管线（规范化 → 通知 → 状态机），带并发限流。
// 超出并发上限的投递排队等待而非丢弃；实例关闭期间不再排队新工作，
// 已入队行的状态标记仍由 Process 内部脱离取消的 context 完成。
func (s *server) processWebhookAsync(rowID, eventType, deliveryID string, body []byte) {
	s.safeGo("webhook_process", func() {
		if s.acquireWebhookSlot(s.dependencies.Background) {
			defer s.releaseWebhookSlot()
			s.webhookSvc.Process(rowID, eventType, deliveryID, body)
		}
	})
}

// New 创建管理 API HTTP Handler。
func New(dependencies Dependencies) http.Handler {
	if dependencies.Logger == nil {
		dependencies.Logger = slog.Default()
	}
	if dependencies.Ready == nil {
		dependencies.Ready = ReadyCheckFunc(func(context.Context) error { return nil })
	}
	if dependencies.LoginLimiter == nil {
		dependencies.LoginLimiter = auth.NewLoginLimiter(nil)
	}
	if strings.TrimSpace(dependencies.SchemaVersion) == "" {
		dependencies.SchemaVersion = "unknown"
	}

	s := &server{
		dependencies:   dependencies,
		secureCookies:  usesSecureCookies(dependencies.Config.HTTP.PublicBaseURL),
		trustedProxies: parseTrustedSubnets(dependencies.Config.HTTP.TrustedProxies),
		webhookSem:     make(chan struct{}, webhookProcessConcurrency),
		loginSem:       make(chan struct{}, 3),
		totpTickets:    auth.NewTOTPTicketManager(3 * time.Minute),
		webhookSvc: &webhooksvc.Service{
			Store:      dependencies.Store,
			Logger:     dependencies.Logger,
			Evaluator:  dependencies.Aggregator,
			AI:         dependencies.AI,
			Background: dependencies.Background,
			OnFailed:   MetricsIncWebhookFailed,
		},
	}
	// 若运行时 Public Base URL 来自管理台，启动后仍以当前快照为准（见 cookiesSecure）。
	router := chi.NewRouter()
	router.Use(s.requestIDMiddleware)
	router.Use(s.realIPMiddleware)
	router.Use(s.accessLogMiddleware)
	router.Use(s.recoveryMiddleware)
	router.Use(s.securityHeadersMiddleware)
	router.Use(agentLinkHeadersMiddleware)

	router.Get("/health/live", s.handleLive)
	router.Get("/health/ready", s.handleReady)
	if dependencies.Config.Metrics.Enabled {
		router.Get("/metrics", s.handleMetrics)
	}
	router.Post(githubx.WebhookPath, s.handleGitHubWebhook)

	// Agent 发现端点（RFC 8288 / 9727 / 9728 / 8414、sitemap、MCP、Auth.md）。
	router.Get("/robots.txt", s.handleRobotsTXT)
	router.Get("/sitemap.xml", s.handleSitemapXML)
	router.Get("/auth.md", s.handleAuthMD)
	router.Get("/openapi.json", s.handleOpenAPIJSON)
	router.Get("/.well-known/api-catalog", s.handleWellKnownAPICatalog)
	router.Get("/.well-known/oauth-authorization-server", s.handleWellKnownOAuthAuthorizationServer)
	router.Get("/.well-known/oauth-protected-resource", s.handleWellKnownOAuthProtectedResource)
	router.Get("/.well-known/agent-skills/index.json", s.handleWellKnownAgentSkillsIndex)
	router.Get("/.well-known/agent-skills/reposentinel-api/SKILL.md", s.handleAgentSkillsArtifact)
	router.Get("/.well-known/mcp/server-card.json", s.handleWellKnownMCPCard)
	router.Get("/oauth/jwks", s.handleOAuthJWKS)
	router.Get("/oauth/authorize", s.handleOAuthAuthorize)
	router.Post("/oauth/authorize", s.handleOAuthAuthorize)
	router.Post("/oauth/token", s.handleOAuthToken)
	router.Post("/mcp", s.authenticationMiddleware(s.storeGuardMiddleware(http.HandlerFunc(s.handleMCP))).ServeHTTP)
	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/setup/status", s.handleSetupStatus)
		api.Post("/setup", s.handleSetup)
		api.Post("/auth/login", s.handleLogin)
		api.Post("/auth/login/2fa", s.handleLogin2FA)
		// 公开极简构建信息：登录页页脚展示真实版本（不暴露配置细节）。
		api.Get("/system/build-info", s.handleBuildInfo)

		api.Group(func(protected chi.Router) {
			protected.Use(s.authenticationMiddleware)
			// Store 统一守卫：未装配时受保护路由一律 503，避免各 handler 遗漏 nil 检查。
			protected.Use(s.storeGuardMiddleware)
			protected.Get("/auth/session", s.handleSession)
			protected.Get("/system/version", s.handleVersion)
			protected.Get("/dashboard", s.handleDashboard)
			protected.Get("/stats/star-trend", s.handleStarTrend)
			protected.Get("/repositories", s.handleListRepositories)
			protected.Post("/repositories/external", s.handleAddExternalRepository)
			protected.Get("/work-items", s.handleListWorkItems)
			protected.Get("/workflow-runs", s.handleListWorkflowRuns)
			protected.Get("/security-alerts", s.handleListSecurityAlerts)
			protected.Get("/events", s.handleListEvents)
			protected.Get("/notifications/outbox", s.handleListOutbox)
			protected.Get("/notifications/channels", s.handleListChannels)
			protected.Get("/github/installations", s.handleListInstallations)
			protected.Get("/github/config", s.handleGetGitHubConfig)
			protected.Get("/ai/config", s.handleGetAIConfig)
			protected.Get("/starred-releases/config", s.handleGetStarredReleasesConfig)
			protected.Get("/starred-releases/trackers", s.handleListStarredTrackers)
			protected.Get("/system/settings", s.handleGetSettings)
			protected.Get("/admin/2fa", s.handleGet2FA)
			protected.Group(func(mutating chi.Router) {
				mutating.Use(s.csrfMiddleware)
				mutating.Post("/auth/logout", s.handleLogout)
				mutating.Post("/auth/password", s.handleChangePassword)
				mutating.Post("/admin/2fa/setup", s.handleSetup2FA)
				mutating.Post("/admin/2fa/enable", s.handleEnable2FA)
				mutating.Post("/admin/2fa/disable", s.handleDisable2FA)
				mutating.Put("/notifications/channels/{type}", s.handleUpsertChannel)
				mutating.Post("/notifications/channels/{type}/test", s.handleTestChannel)
				mutating.Delete("/notifications/channels/{type}", s.handleDeleteChannel)
				mutating.Patch("/notifications/channels/{type}/toggle", s.handleToggleChannel)
				mutating.Post("/notifications/outbox/{id}/retry", s.handleRetryOutbox)
				// 批量重试失败投递（固定段优先于 {id} 参数段匹配）。
				mutating.Post("/notifications/outbox/retry-dead", s.handleRetryAllOutboxDead)
				mutating.Post("/repositories/{id}/activate", s.handleActivateRepository)
				mutating.Post("/repositories/{id}/reconcile", s.handleReconcileRepository)
				mutating.Patch("/repositories/{id}/settings", s.handleUpdateRepositorySettings)
				mutating.Delete("/repositories/{id}", s.handleDeleteRepository)
				mutating.Patch("/work-items/{id}/ignored", s.handleSetWorkItemIgnored)
				mutating.Patch("/workflow-runs/{id}/ignored", s.handleSetWorkflowRunIgnored)
				mutating.Patch("/security-alerts/{id}/ignored", s.handleSetSecurityAlertIgnored)
				mutating.Post("/sync/reconcile", s.handleReconcileAll)
				mutating.Put("/system/settings", s.handlePutSettings)
				mutating.Put("/github/config", s.handlePutGitHubConfig)
				mutating.Put("/ai/config", s.handlePutAIConfig)
				mutating.Post("/ai/test", s.handleTestAIConfig)
				mutating.Put("/starred-releases/config", s.handlePutStarredReleasesConfig)
				mutating.Post("/starred-releases/sync", s.handleSyncStarredReleases)
				mutating.Post("/starred-releases/trackers/{id}/state", s.handleSetStarredTrackerState)
				mutating.Post("/github/sync-repositories", s.handleSyncInstallationRepositories)
				mutating.Post("/system/version/check", s.handleVersionCheck)
			})
		})
	})
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
	})
	spa := newSPAHandler(dependencies.Frontend, notFound)
	router.NotFound(s.markdownNegotiationMiddleware(spa).ServeHTTP)
	// 路径存在但方法不符：返回 405 与语义化错误码（此前与 404 混为一体，MCP/OAuth
	// 客户端按方法探测时被误导）。已知 POST-only 端点补 Allow 提示（RFC 9110 §15.5.6）。
	router.MethodNotAllowed(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/mcp", "/oauth/token", "/webhooks/github":
			w.Header().Set("Allow", http.MethodPost)
		}
		s.writeAPIError(w, r, http.StatusMethodNotAllowed, errorCodeMethodNotAllowed, nil)
	}).ServeHTTP)
	return router
}

func usesSecureCookies(publicBaseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(publicBaseURL))
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

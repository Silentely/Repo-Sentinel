package httpapi

import (
	"context"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync/atomic"

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
}

type server struct {
	dependencies  Dependencies
	secureCookies bool
	// webhookSvc 承担 Webhook 后台管线（规范化 → 通知 → 状态机），装配一次复用。
	webhookSvc *webhooksvc.Service
	// reconcileAllRunning 防止全量对账并发触发（对账会大量调用 GitHub API 并写库）。
	reconcileAllRunning atomic.Bool
	// webhookSem 控制 webhook 后台处理并发；容量见 webhookProcessConcurrency。
	webhookSem chan struct{}
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
		dependencies:  dependencies,
		secureCookies: usesSecureCookies(dependencies.Config.HTTP.PublicBaseURL),
		webhookSem:    make(chan struct{}, webhookProcessConcurrency),
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
	router.Use(securityHeadersMiddleware)
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
	router.Post("/mcp", s.authenticationMiddleware(http.HandlerFunc(s.handleMCP)).ServeHTTP)
	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/setup/status", s.handleSetupStatus)
		api.Post("/setup", s.handleSetup)
		api.Post("/auth/login", s.handleLogin)

		api.Group(func(protected chi.Router) {
			protected.Use(s.authenticationMiddleware)
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
			protected.Get("/system/settings", s.handleGetSettings)
			protected.Group(func(mutating chi.Router) {
				mutating.Use(s.csrfMiddleware)
				mutating.Post("/auth/logout", s.handleLogout)
				mutating.Post("/auth/password", s.handleChangePassword)
				mutating.Put("/notifications/channels/{type}", s.handleUpsertChannel)
				mutating.Post("/notifications/channels/{type}/test", s.handleTestChannel)
				mutating.Delete("/notifications/channels/{type}", s.handleDeleteChannel)
				mutating.Patch("/notifications/channels/{type}/toggle", s.handleToggleChannel)
				mutating.Post("/notifications/outbox/{id}/retry", s.handleRetryOutbox)
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
	router.MethodNotAllowed(notFound.ServeHTTP)
	return router
}

func usesSecureCookies(publicBaseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(publicBaseURL))
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

package httpapi

import (
	"context"
	"io/fs"
	"log/slog"
	"net/http"
	"net/url"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
	"github.com/go-chi/chi/v5"
)

const (
	// SessionCookieName 是服务端 Session 原始令牌的固定 Cookie 名。
	SessionCookieName = "reposentinel_session"
	// CSRFCookieName 是双提交 CSRF 原始令牌的固定 Cookie 名。
	CSRFCookieName = "reposentinel_csrf"
	// CSRFHeaderName 是写请求携带双提交令牌的固定 Header 名。
	CSRFHeaderName = "X-CSRF-Token"
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
}

type server struct {
	dependencies  Dependencies
	secureCookies bool
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
	}
	// 若运行时 Public Base URL 来自管理台，启动后仍以当前快照为准（见 cookiesSecure）。
	router := chi.NewRouter()
	router.Use(s.requestIDMiddleware)
	router.Use(s.realIPMiddleware)
	router.Use(s.accessLogMiddleware)
	router.Use(s.recoveryMiddleware)
	router.Use(securityHeadersMiddleware)

	router.Get("/health/live", s.handleLive)
	router.Get("/health/ready", s.handleReady)
	if dependencies.Config.Metrics.Enabled {
		router.Get("/metrics", s.handleMetrics)
	}
	router.Post("/webhooks/github", s.handleGitHubWebhook)
	router.Route("/api/v1", func(api chi.Router) {
		api.Get("/setup/status", s.handleSetupStatus)
		api.Post("/setup", s.handleSetup)
		api.Post("/auth/login", s.handleLogin)

		api.Group(func(protected chi.Router) {
			protected.Use(s.authenticationMiddleware)
			protected.Get("/auth/session", s.handleSession)
			protected.Get("/system/version", s.handleVersion)
			protected.Get("/dashboard", s.handleDashboard)
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
				mutating.Patch("/work-items/{id}/ignored", s.handleSetWorkItemIgnored)
				mutating.Patch("/workflow-runs/{id}/ignored", s.handleSetWorkflowRunIgnored)
				mutating.Patch("/security-alerts/{id}/ignored", s.handleSetSecurityAlertIgnored)
				mutating.Post("/sync/reconcile", s.handleReconcileAll)
				mutating.Put("/system/settings", s.handlePutSettings)
				mutating.Put("/github/config", s.handlePutGitHubConfig)
				mutating.Post("/github/sync-repositories", s.handleSyncInstallationRepositories)
				mutating.Post("/system/version/check", s.handleVersionCheck)
			})
		})
	})
	notFound := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
	})
	router.NotFound(newSPAHandler(dependencies.Frontend, notFound).ServeHTTP)
	router.MethodNotAllowed(notFound.ServeHTTP)
	return router
}

func usesSecureCookies(publicBaseURL string) bool {
	parsed, err := url.Parse(strings.TrimSpace(publicBaseURL))
	return err == nil && strings.EqualFold(parsed.Scheme, "https")
}

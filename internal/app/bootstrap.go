package app

import (
	"context"
	"encoding/json"
	"errors"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/digest"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/httpapi"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
	webassets "github.com/Silentely/Repo-Sentinel/web"
	"github.com/oklog/ulid/v2"
)

type buildDependencies struct {
	openStore          func(context.Context, config.DatabaseConfig) (store.Store, error)
	validateEncryption func(context.Context, store.Store, config.EncryptionConfig) (*cryptox.KeyRing, error)
	openFrontend       func() (fs.FS, error)
	newLogger          func(config.LoggingConfig) *slog.Logger
	newHTTPServer      func(string, http.Handler) httpRuntime
}

func defaultBuildDependencies() buildDependencies {
	return buildDependencies{
		openStore:          store.Open,
		validateEncryption: validateEncryptionKey,
		openFrontend:       webassets.Files,
		newLogger:          newLogger,
		newHTTPServer: func(addr string, handler http.Handler) httpRuntime {
			return httpServerRuntime{&http.Server{
				Addr:              addr,
				Handler:           handler,
				ReadHeaderTimeout: 10 * time.Second,
				ReadTimeout:       30 * time.Second,
				// WriteTimeout 45s：为 AI 连通性测试（同步 handler，探测上限 30s）与
				// 慢响应留出余量；读侧仍由 ReadTimeout（30s）保护，无需同步放宽。
				WriteTimeout: 45 * time.Second,
				IdleTimeout:  60 * time.Second,
			}}
		},
	}
}

// Build 按配置、数据库、主密钥、管理员、服务与 HTTP 的顺序装配 App。
func Build(ctx context.Context, cfg config.Config) (*App, error) {
	return buildWithDependencies(ctx, cfg, defaultBuildDependencies())
}

func buildWithDependencies(ctx context.Context, cfg config.Config, dependencies buildDependencies) (_ *App, returnedErr error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	logger := dependencies.newLogger(cfg.Logging)
	data, err := dependencies.openStore(ctx, cfg.Database)
	if err != nil {
		return nil, mapStoreOpenError(err)
	}
	defer func() {
		if returnedErr != nil {
			_ = data.Close()
		}
	}()

	keyRing, err := dependencies.validateEncryption(ctx, data, cfg.Encryption)
	if err != nil {
		if errors.Is(err, cryptox.ErrInvalidEncryptionKey) {
			return nil, newPublicError(
				"invalid_encryption_key",
				"主密钥格式非法：需 64 位 hex 或 32 字节 base64（与数据库无关，请检查 REPOSENTINEL_ENCRYPTION_KEY 取值）。",
				cryptox.ErrInvalidEncryptionKey,
			)
		}
		if errors.Is(err, cryptox.ErrEncryptionKeyMismatch) {
			return nil, newPublicError(
				"encryption_key_mismatch",
				"无法验证加密主密钥，请检查当前/上一把密钥与数据库是否匹配。",
				cryptox.ErrEncryptionKeyMismatch,
			)
		}
		return nil, newPublicError("database_unavailable", "无法校验数据库中的加密状态。", err)
	}
	frontend, err := dependencies.openFrontend()
	if err != nil {
		return nil, newPublicError("frontend_unavailable", "无法加载管理控制台资源。", err)
	}

	adminService := auth.NewAdminService(data, auth.NewPasswordHasher())
	if err := bootstrapConfiguredAdmin(ctx, data, adminService, cfg); err != nil {
		return nil, err
	}
	sessionService := auth.NewSessionService(data, nil, nil, cfg.Admin.SessionTTL)
	readiness := &readinessState{}
	// workerCancel 在成功装配前由本函数持有；失败路径必须取消，成功后交给 App.Close/Run。
	workerCtx, workerCancel := context.WithCancel(context.Background())
	workerOwned := true
	defer func() {
		if workerOwned {
			workerCancel()
		}
	}()

	ghClient := githubx.NewAppClient(cfg.GitHub.AppID, cfg.GitHub.PrivateKeyPath)
	ghRuntime := newRuntimeFromEnv(cfg, ghClient)
	if err := githubx.MergeFromStore(ctx, data, keyRing, ghRuntime); err != nil {
		return nil, newPublicError("database_unavailable", "无法加载 GitHub 运行时配置。", err)
	}
	reconciler := &syncx.Reconciler{
		Store: data, GitHub: ghClient, Logger: logger, MaxPages: 3,
		OnRun: httpapi.MetricsIncReconcileRuns,
	}
	external := &syncx.ExternalPoller{
		Store:  data,
		Client: &githubx.PublicClient{PAT: cfg.GitHub.ExternalPAT.Reveal()},
		Logger: logger,
	}
	aggWindow := cfg.Aggregation.Window
	if aggWindow <= 0 {
		aggWindow = 60 * time.Second
	}
	aggBurstN := cfg.Aggregation.BurstThreshold
	if aggBurstN <= 0 {
		aggBurstN = 15
	}
	aggBurstW := cfg.Aggregation.BurstWindow
	if aggBurstW <= 0 {
		aggBurstW = 5 * time.Minute
	}
	aggregator := rules.NewAggregator(data, aggWindow, aggBurstN, aggBurstW)
	// AI 运行时：env 基线 + 数据库补缺（管理台可编辑），物化为客户端注入各服务。
	aiRuntime := ai.RuntimeFromEnv(cfg.AI)
	if err := ai.MergeFromStore(ctx, data, keyRing, aiRuntime); err != nil {
		return nil, newPublicError("database_unavailable", "无法加载 AI 运行时配置。", err)
	}
	aiClient := aiRuntime.Client()
	aiClient.Logger = logger
	aggregator.AI = aiClient
	aggregator.Logger = logger
	digestGen := &digest.Generator{Store: data, AI: aiClient, Logger: logger}

	starred := &syncx.StarredReleasePoller{
		Store:  data,
		GitHub: ghClient,
		// star 枚举复用 external_pat：配置了 PAT 时按 5000 次/小时配额拉取，匿名仅 60 次/小时易限流。
		Public: &githubx.PublicClient{PAT: cfg.GitHub.ExternalPAT.Reveal()},
		// Engine 直连实时通知决策（不聚合：release 低频单条，立即投递）。
		Engine: &rules.Engine{Store: data, AI: aiClient, Logger: logger},
		Logger: logger,
	}
	// 用户名初始值：settings 优先，配置（env/yaml）兜底并写入 settings 供设置页回显。
	if syncx.StarredUsername(ctx, data.Settings()) == "" && strings.TrimSpace(cfg.GitHub.StarredUsername) != "" {
		raw, _ := json.Marshal(strings.TrimSpace(cfg.GitHub.StarredUsername))
		if _, err := data.Settings().Upsert(ctx, store.SystemSetting{
			ID: ulid.Make().String(), Key: syncx.SettingStarredUsername,
			ValueJSON: raw, UpdatedAt: time.Now().UTC(), UpdatedBy: "bootstrap",
		}); err != nil {
			return nil, newPublicError("database_unavailable", "无法写入 star 追踪用户名。", err)
		}
	}
	scheduler := &syncx.Scheduler{
		Reconciler: reconciler, External: external, Starred: starred, Digest: digestGen, Logger: logger,
	}
	build := buildinfo.Current()
	updateChecker := &updatecheck.Checker{
		Enabled:  cfg.UpdateCheck.Enabled,
		CheckURL: cfg.UpdateCheck.URL,
		Token:    cfg.UpdateCheck.Token.Reveal(),
		Current:  build.Version,
		Logger:   logger,
	}

	handler := httpapi.New(httpapi.Dependencies{
		Config:         cfg,
		Store:          data,
		AdminStore:     data.Admins(),
		AdminService:   adminService,
		SessionService: sessionService,
		CSRF:           auth.NewCSRFTokens(nil),
		LoginLimiter:   auth.NewLoginLimiter(nil),
		BuildInfo:      build,
		Ready:          readiness,
		Logger:         logger,
		SchemaVersion:  SupportedSchemaVersion,
		Frontend:       frontend,
		KeyRing:        keyRing,
		Aggregator:     aggregator,
		Reconciler:     reconciler,
		UpdateChecker:  updateChecker,
		GitHubRuntime:  ghRuntime,
		Background:     workerCtx,
		AI:             aiClient,
		AIRuntime:      aiRuntime,
		StarredPoller:  starred,
	})
	if err := bootstrapNotifyChannels(ctx, logger, data, keyRing, cfg); err != nil {
		return nil, err
	}
	built := &App{
		data:            data,
		keyRing:         keyRing,
		workerCancel:    workerCancel,
		workerCtx:       workerCtx,
		adminService:    adminService,
		sessionService:  sessionService,
		httpServer:      dependencies.newHTTPServer(cfg.HTTP.Addr, handler),
		readiness:       readiness,
		logger:          logger,
		cleanupInterval: defaultCleanupInterval,
		scheduler:       scheduler,
		httpAddr:        cfg.HTTP.Addr,
		databaseDriver:  cfg.Database.Driver,
	}
	workerOwned = false
	return built, nil
}

func bootstrapConfiguredAdmin(
	ctx context.Context,
	data store.Store,
	service *auth.AdminService,
	cfg config.Config,
) error {
	_, err := data.Admins().GetOnly(ctx)
	switch {
	case err == nil:
		return nil
	case !errors.Is(err, store.ErrNotFound):
		return newPublicError("database_unavailable", "无法读取管理员初始化状态。", err)
	}
	if strings.TrimSpace(cfg.Admin.Username) == "" {
		return nil
	}
	_, err = service.BootstrapAdmin(ctx, cfg.Admin.Username, cfg.Admin.Password.Reveal())
	return err
}

func newLogger(cfg config.LoggingConfig) *slog.Logger {
	level := new(slog.LevelVar)
	switch cfg.Level {
	case "debug":
		level.Set(slog.LevelDebug)
	case "warn":
		level.Set(slog.LevelWarn)
	case "error":
		level.Set(slog.LevelError)
	default:
		level.Set(slog.LevelInfo)
	}
	options := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.Format == "text" {
		handler = slog.NewTextHandler(os.Stderr, options)
	} else {
		handler = slog.NewJSONHandler(os.Stderr, options)
	}
	return slog.New(handler)
}

func mapStoreOpenError(err error) error {
	if err == nil {
		return newPublicError("database_unavailable", "无法打开数据库。", nil)
	}
	var openErr *store.OpenError
	if errors.As(err, &openErr) && openErr != nil {
		msg := "无法打开数据库。"
		if openErr.Reason != "" {
			msg = "无法打开数据库：" + openErr.Reason + "。"
		}
		code := "database_unavailable"
		if openErr.Kind == "migrate" {
			code = "migration_failed"
			if openErr.Reason == "" {
				msg = "数据库迁移失败。"
			}
		}
		return newPublicError(code, msg, err)
	}
	if errors.Is(err, store.ErrMigrationFailed) || strings.Contains(err.Error(), "database migration failed") {
		return newPublicError("migration_failed", "数据库迁移失败。", err)
	}
	return newPublicError("database_unavailable", "无法打开数据库。", err)
}

// bootstrapNotifyChannels 将环境中的 Telegram/HTTP 配置物化为渠道行（每类最多启用 1 个）。
func bootstrapNotifyChannels(ctx context.Context, logger *slog.Logger, data store.Store, keyRing *cryptox.KeyRing, cfg config.Config) error {
	if keyRing == nil {
		return nil
	}
	const aad = notify.AAD
	if tok := cfg.Notify.Telegram.Token.Reveal(); tok != "" && cfg.Notify.Telegram.ChatID != "" {
		if _, err := data.Channels().GetEnabledByType(ctx, store.ChannelTelegram); errors.Is(err, store.ErrNotFound) {
			env, err := keyRing.Encrypt(ctx, []byte(tok), []byte(aad))
			if err != nil {
				return newPublicError("encryption_failed", "无法加密 Telegram Token。", err)
			}
			ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
				ChannelType: store.ChannelTelegram, Name: "Telegram", Enabled: true,
				Target: cfg.Notify.Telegram.ChatID, SecretEnvelope: env,
				DigestEnabled: true, // 环境种子渠道与历史默认一致：全订阅 + 收每日汇总
			})
			if err != nil {
				return newPublicError("database_unavailable", "无法初始化 Telegram 渠道。", err)
			}
			// 同类型去重失败会留下多实例并存（下次写入再收敛），低危但留痕。
			if err := data.Channels().DisableOthersOfType(ctx, store.ChannelTelegram, ch.ID); err != nil && logger != nil {
				logger.Warn("seed channel dedupe failed", "channel_type", store.ChannelTelegram, "error_code", "channel_disable_failed", "error", err.Error())
			}
		}
	}
	if url := strings.TrimSpace(cfg.Notify.HTTPWebhook.URL); url != "" {
		if _, err := data.Channels().GetEnabledByType(ctx, store.ChannelHTTPWebhook); errors.Is(err, store.ErrNotFound) {
			secretEnv := ""
			if sec := cfg.Notify.HTTPWebhook.Secret.Reveal(); sec != "" {
				env, err := keyRing.Encrypt(ctx, []byte(sec), []byte(aad))
				if err != nil {
					return newPublicError("encryption_failed", "无法加密 HTTP Webhook Secret。", err)
				}
				secretEnv = env
			}
			ch, err := data.Channels().Upsert(ctx, store.NotificationChannel{
				ChannelType: store.ChannelHTTPWebhook, Name: "HTTP Webhook", Enabled: true,
				Target: url, SecretEnvelope: secretEnv, AllowPrivate: cfg.Notify.HTTPWebhook.AllowPrivate,
				DigestEnabled: true, // 环境种子渠道与历史默认一致：全订阅 + 收每日汇总
			})
			if err != nil {
				return newPublicError("database_unavailable", "无法初始化 HTTP Webhook 渠道。", err)
			}
			// 同类型去重失败会留下多实例并存（下次写入再收敛），低危但留痕。
			if err := data.Channels().DisableOthersOfType(ctx, store.ChannelHTTPWebhook, ch.ID); err != nil && logger != nil {
				logger.Warn("seed channel dedupe failed", "channel_type", store.ChannelHTTPWebhook, "error_code", "channel_disable_failed", "error", err.Error())
			}
		}
	}
	return nil
}

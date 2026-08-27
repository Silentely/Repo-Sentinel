package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/httpapi"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Run 启动 HTTP、通知 Worker 与 Session 清理，并在取消时按 gracefulShutdownTimeout 预算优雅关闭。
func (a *App) Run(ctx context.Context) error {
	if a == nil || a.httpServer == nil {
		return newPublicError("internal_error", "应用尚未完成装配。", nil)
	}
	if ctx == nil {
		ctx = context.Background()
	}
	workerCtx := a.workerCtx
	if workerCtx == nil {
		var cancel context.CancelFunc
		workerCtx, cancel = context.WithCancel(context.Background())
		a.workerCancel = cancel
		a.workerCtx = workerCtx
	}
	workerDone := make(chan struct{})
	go func() {
		defer close(workerDone)
		a.runSessionCleanup(workerCtx)
	}()
	retentionDone := make(chan struct{})
	go func() {
		defer close(retentionDone)
		a.runRetentionCleanup(workerCtx)
	}()
	notifyDone := make(chan struct{})
	go func() {
		defer close(notifyDone)
		(&notify.Worker{
			Store:   a.data,
			KeyRing: a.keyRing,
			Logger:  a.logger,
			OnSent:  httpapi.MetricsIncOutboxSent,
			OnDead:  httpapi.MetricsIncOutboxDead,
		}).Run(workerCtx, 5*time.Second)
	}()
	schedDone := make(chan struct{})
	go func() {
		defer close(schedDone)
		if a.scheduler != nil {
			a.scheduler.Run(workerCtx)
		}
	}()

	serverResult := make(chan error, 1)
	// 先显式监听再宣告就绪：此前 Build 末尾即 Set(true) + ready 日志，
	// 端口占用时进程先报 ready 再以 http_server_failed 退出（假就绪窗口）。
	ln, err := net.Listen("tcp", a.httpServer.Addr())
	if err != nil {
		if a.workerCancel != nil {
			a.workerCancel()
		}
		<-workerDone
		<-retentionDone
		<-notifyDone
		<-schedDone
		_ = a.Close()
		return newPublicError("http_server_failed", "HTTP Server 监听失败。", err)
	}
	if a.readiness != nil {
		a.readiness.Set(true)
	}
	info := buildinfo.Current()
	a.logger.Info(
		"reposentinel ready",
		"version", info.Version,
		"git_sha", info.GitSHA,
		"build_time", info.BuildTime,
		"build_channel", info.BuildChannel,
		"database_driver", a.databaseDriver,
		"schema_version", SupportedSchemaVersion,
		"http_addr", a.httpAddr,
	)
	go func() {
		serverResult <- a.httpServer.Serve(ln)
	}()

	var runErr error
	select {
	case <-ctx.Done():
	case err := <-serverResult:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			runErr = newPublicError("http_server_failed", "HTTP Server 运行失败。", err)
		}
	}

	if a.readiness != nil {
		a.readiness.Set(false)
	}
	if a.workerCancel != nil {
		a.workerCancel()
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), gracefulShutdownTimeout)
	shutdownErr := a.httpServer.Shutdown(shutdownCtx)
	cancelShutdown()
	<-workerDone
	<-retentionDone
	<-notifyDone
	<-schedDone
	closeErr := a.Close()

	if runErr != nil {
		return runErr
	}
	if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
		return newPublicError("shutdown_failed", fmt.Sprintf("HTTP Server 未能在 %s 内关闭。", gracefulShutdownTimeout), shutdownErr)
	}
	if closeErr != nil {
		return newPublicError("database_unavailable", "关闭数据库资源失败。", closeErr)
	}
	return nil
}

func (a *App) runSessionCleanup(ctx context.Context) {
	if a.sessionService == nil || a.cleanupInterval <= 0 {
		<-ctx.Done()
		return
	}
	ticker := time.NewTicker(a.cleanupInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			deleted, err := a.sessionService.CleanupExpired(ctx)
			if err != nil {
				if a.logger != nil {
					a.logger.Error("session cleanup failed", "error_code", "database_unavailable", "error", err.Error())
				}
				continue
			}
			if a.logger != nil {
				// 无论删除量都留痕（Debug）：排查"过期 Session 有没有清"不依赖删除数。
				a.logger.Debug("session cleanup ran", "deleted", deleted)
			}
		}
	}
}

// runRetentionCleanup 按系统设置定期清理过期事件、终态 Outbox 与旧 delivery。
func (a *App) runRetentionCleanup(ctx context.Context) {
	if a.data == nil {
		<-ctx.Done()
		return
	}
	// 启动后短暂延迟，避免与迁移/对账启动风暴重叠。
	startup := time.NewTimer(2 * time.Minute)
	ticker := time.NewTicker(defaultRetentionCleanupInterval)
	defer startup.Stop()
	defer ticker.Stop()

	runOnce := func() {
		policy := a.loadRetentionPolicy(ctx)
		result, err := a.data.CleanupRetention(ctx, policy, time.Now().UTC())
		if err != nil {
			if a.logger != nil {
				a.logger.Error("retention cleanup failed", "error_code", "database_unavailable", "error", err.Error())
			}
			return
		}
		if a.logger != nil && (result.EventsDeleted > 0 || result.OutboxDeleted > 0 || result.WebhookDeliveriesDeleted > 0) {
			a.logger.Info(
				"retention cleanup completed",
				"events_deleted", result.EventsDeleted,
				"outbox_deleted", result.OutboxDeleted,
				"webhook_deliveries_deleted", result.WebhookDeliveriesDeleted,
				"events_days", policy.EventsDays,
				"outbox_days", policy.OutboxDays,
				"webhook_deliveries_days", policy.WebhookDeliveriesDays,
			)
		} else if a.logger != nil {
			// 无过期数据也留痕（Debug）：排查"清理到底跑没跑、策略是什么"时不必依赖删除量。
			a.logger.Debug(
				"retention cleanup ran",
				"events_deleted", result.EventsDeleted,
				"outbox_deleted", result.OutboxDeleted,
				"webhook_deliveries_deleted", result.WebhookDeliveriesDeleted,
				"events_days", policy.EventsDays,
				"outbox_days", policy.OutboxDays,
				"webhook_deliveries_days", policy.WebhookDeliveriesDays,
			)
		}
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-startup.C:
			runOnce()
		case <-ticker.C:
			runOnce()
		}
	}
}

func (a *App) loadRetentionPolicy(ctx context.Context) store.RetentionPolicy {
	policy := store.DefaultRetentionPolicy()
	if a.data == nil {
		return policy
	}
	policy.EventsDays = a.readRetentionDays(ctx, settingRetentionEventsDays, policy.EventsDays)
	policy.OutboxDays = a.readRetentionDays(ctx, settingRetentionOutboxDays, policy.OutboxDays)
	policy.WebhookDeliveriesDays = a.readRetentionDays(ctx, settingRetentionDeliveriesDays, policy.WebhookDeliveriesDays)
	return policy
}

func (a *App) readRetentionDays(ctx context.Context, key string, fallback int) int {
	setting, err := a.data.Settings().Get(ctx, key)
	if err != nil {
		return fallback
	}
	var raw any
	if err := json.Unmarshal(setting.ValueJSON, &raw); err != nil {
		return fallback
	}
	days := intFromAny(raw)
	if days < 0 {
		return fallback
	}
	if days > 3650 {
		return 3650
	}
	return days
}

func intFromAny(v any) int {
	// 收敛逻辑与设置解析（CoerceInt）共用，非法值统一回退 -1。
	n, ok := store.CoerceInt(v)
	if !ok {
		return -1
	}
	return n
}

// ResetAdminPassword 打开本地依赖、校验主密钥并执行审计化密码恢复。
func ResetAdminPassword(ctx context.Context, cfg config.Config, newPassword string) (returnedErr error) {
	if err := cfg.Validate(); err != nil {
		return err
	}
	data, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return mapStoreOpenError(err)
	}
	defer func() {
		if closeErr := data.Close(); returnedErr == nil && closeErr != nil {
			returnedErr = newPublicError("database_unavailable", "关闭数据库资源失败。", closeErr)
		}
	}()
	if _, err := validateEncryptionKey(ctx, data, cfg.Encryption); err != nil {
		if errors.Is(err, cryptox.ErrEncryptionKeyMismatch) {
			return newPublicError(
				"encryption_key_mismatch",
				"无法验证加密主密钥，请检查当前/上一把密钥与数据库是否匹配。",
				cryptox.ErrEncryptionKeyMismatch,
			)
		}
		return newPublicError("database_unavailable", "无法校验数据库中的加密状态。", err)
	}
	service := auth.NewAdminService(data, auth.NewPasswordHasher())
	if err := service.ResetPassword(ctx, newPassword); err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return newPublicError("not_found", "数据库中尚无管理员账户。", err)
		}
		return err
	}
	return nil
}

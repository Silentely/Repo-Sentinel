package app

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Run 启动 HTTP、通知 Worker 与 Session 清理，并在取消时按 30 秒预算优雅关闭。
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
	notifyDone := make(chan struct{})
	go func() {
		defer close(notifyDone)
		(&notify.Worker{
			Store:   a.data,
			KeyRing: a.keyRing,
			Logger:  a.logger,
		}).Run(workerCtx, 5*time.Second)
	}()

	serverResult := make(chan error, 1)
	go func() {
		serverResult <- a.httpServer.ListenAndServe()
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
	<-notifyDone
	closeErr := a.Close()

	if runErr != nil {
		return runErr
	}
	if shutdownErr != nil && !errors.Is(shutdownErr, http.ErrServerClosed) {
		return newPublicError("shutdown_failed", "HTTP Server 未能在 30 秒内关闭。", shutdownErr)
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
			if _, err := a.sessionService.CleanupExpired(ctx); err != nil && a.logger != nil {
				a.logger.Error("session cleanup failed", "error_code", "database_unavailable")
			}
		}
	}
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

package app

import (
	"context"
	"errors"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
)

const (
	// SupportedSchemaVersion 是当前二进制支持的最新 Atlas migration 版本。
	SupportedSchemaVersion  = "20260728000100"
	gracefulShutdownTimeout = 30 * time.Second
	defaultCleanupInterval  = 15 * time.Minute
	// defaultRetentionCleanupInterval 历史数据清理周期，默认每日一次。
	defaultRetentionCleanupInterval = 24 * time.Hour
)

// 系统设置中的保留天数键。
const (
	settingRetentionEventsDays     = "retention.events_days"
	settingRetentionOutboxDays     = "retention.outbox_days"
	settingRetentionDeliveriesDays = "retention.webhook_deliveries_days"
)

type httpRuntime interface {
	ListenAndServe() error
	Shutdown(context.Context) error
}

// App 保存完成装配的运行时及其生命周期资源。
type App struct {
	data            store.Store
	keyRing         *cryptox.KeyRing
	adminService    *auth.AdminService
	sessionService  *auth.SessionService
	httpServer      httpRuntime
	readiness       *readinessState
	logger          *slog.Logger
	cleanupInterval time.Duration
	workerCtx       context.Context
	workerCancel    context.CancelFunc
	scheduler       *syncx.Scheduler
	closeOnce       sync.Once
	closeErr        error
}

// Close 幂等关闭 App 持有的资源。
func (a *App) Close() error {
	if a == nil {
		return nil
	}
	a.closeOnce.Do(func() {
		if a.readiness != nil {
			a.readiness.Set(false)
		}
		if a.workerCancel != nil {
			a.workerCancel()
		}
		if a.data != nil {
			a.closeErr = a.data.Close()
		}
	})
	return a.closeErr
}

type readinessState struct {
	ready atomic.Bool
}

func (r *readinessState) Ready(ctx context.Context) error {
	if ctx != nil {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
	}
	if !r.IsReady() {
		return errors.New("not ready")
	}
	return nil
}

func (r *readinessState) Set(ready bool) {
	r.ready.Store(ready)
}

func (r *readinessState) IsReady() bool {
	return r != nil && r.ready.Load()
}

// PublicError 对 CLI 暴露稳定错误码与不含内部原因的安全说明。
type PublicError struct {
	code    string
	message string
	cause   error
}

func (e *PublicError) Error() string {
	if e == nil {
		return "internal_error"
	}
	return e.code
}

// ErrorCode 返回稳定错误码。
func (e *PublicError) ErrorCode() string {
	if e == nil || e.code == "" {
		return "internal_error"
	}
	return e.code
}

// PublicMessage 返回可安全展示给本机管理员的说明。
func (e *PublicError) PublicMessage() string {
	if e == nil || e.message == "" {
		return "命令执行失败。"
	}
	return e.message
}

// Unwrap 保留程序内部的 errors.Is/errors.As 能力，但 CLI 不直接打印该原因。
func (e *PublicError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.cause
}

func newPublicError(code, message string, cause error) *PublicError {
	return &PublicError{code: code, message: message, cause: cause}
}

func errorCodeOf(err error) string {
	type coded interface {
		ErrorCode() string
	}
	var target coded
	if errors.As(err, &target) {
		return target.ErrorCode()
	}
	return "internal_error"
}

package httpapi

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

const (
	errorCodeUnauthorized       = "unauthorized"
	errorCodeForbidden          = "forbidden"
	errorCodeInvalidCredentials = "invalid_credentials"
	errorCodeRateLimited        = "rate_limited"
	errorCodeValidationFailed   = "validation_failed"
	errorCodeNotFound           = "not_found"
	errorCodeConflict           = "conflict"
	errorCodeCSRFFailed         = "csrf_failed"
	errorCodeInternal           = "internal_error"
	// service_unavailable 表示「依赖未装配/不可用」（如 Store 缺失）：
	// 与 internal_error（处理过程异常）区分，前端可据此提示服务端状态而非重试。
	errorCodeServiceUnavailable = "service_unavailable"

	// 业务错误码集中在此定义：处理器与 apiErrorMessage 引用同一常量，
	// 新增错误码只需补一处声明与一处文案。
	errorCodeWebhookNotConfigured   = "webhook_not_configured"
	errorCodeInvalidSignature       = "invalid_signature"
	errorCodeReconcileUnavailable   = "reconcile_unavailable"
	errorCodeEncryptionUnavailable  = "encryption_unavailable"
	errorCodeExternalRepoLimit      = "external_repo_limit"
	errorCodeGitHubFieldLocked      = "github_field_locked"
	errorCodeGitHubAppNotConfigured = "github_app_not_configured"
	errorCodeGitHubNoInstallation   = "github_no_installation"
	errorCodeReconcileInProgress    = "reconcile_in_progress"
	errorCodeAIFieldLocked          = "ai_field_locked"
	errorCodeMethodNotAllowed       = "method_not_allowed"

	// loginRetryAfterSeconds 登录限流响应的 Retry-After 秒数。
	loginRetryAfterSeconds = "12"
)

type apiErrorResponse struct {
	ErrorCode string         `json:"error_code"`
	Message   string         `json:"message"`
	Details   map[string]any `json:"details,omitempty"`
}

func (s *server) writeMappedError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, auth.ErrUnauthorized):
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
	case errors.Is(err, auth.ErrInvalidCredentials):
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, nil)
	case errors.Is(err, auth.ErrCSRFFailed):
		s.writeAPIError(w, r, http.StatusForbidden, errorCodeCSRFFailed, nil)
	case errors.Is(err, auth.ErrValidationFailed):
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
	case errors.Is(err, auth.ErrConflict), errors.Is(err, store.ErrConflict):
		s.writeAPIError(w, r, http.StatusConflict, errorCodeConflict, nil)
	case errors.Is(err, store.ErrNotFound):
		s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
	default:
		s.dependencies.Logger.Error(
			"http request failed",
			"request_id", requestIDFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"error_code", errorCodeInternal,
			"error", err.Error(),
		)
		s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
	}
}

func (s *server) writeAPIError(
	w http.ResponseWriter,
	r *http.Request,
	status int,
	errorCode string,
	details map[string]any,
) {
	w.Header().Set("Cache-Control", "no-store")
	if status == http.StatusTooManyRequests {
		w.Header().Set("Retry-After", loginRetryAfterSeconds)
	}
	// 内部错误与未装配错误都附 request_id，便于对照服务端日志定位。
	if (errorCode == errorCodeInternal || errorCode == errorCodeServiceUnavailable) && details == nil {
		details = map[string]any{"request_id": requestIDFromContext(r.Context())}
	}
	writeJSON(w, status, apiErrorResponse{
		ErrorCode: errorCode,
		Message:   apiErrorMessage(errorCode),
		Details:   details,
	})
}

func apiErrorMessage(errorCode string) string {
	switch errorCode {
	case errorCodeUnauthorized:
		return "登录已失效，请重新登录。"
	case errorCodeForbidden:
		return "当前请求不允许执行此操作。"
	case errorCodeInvalidCredentials:
		return "无法验证登录，请检查用户名与密码。"
	case errorCodeRateLimited:
		return "登录尝试过于频繁，请稍后再试。"
	case errorCodeValidationFailed:
		return "请求内容未通过校验。"
	case errorCodeNotFound:
		return "请求的资源不存在。"
	case errorCodeConflict:
		return "当前状态与请求冲突。"
	case errorCodeCSRFFailed:
		return "安全校验失败，请刷新页面后重试。"
	case errorCodeWebhookNotConfigured:
		return "尚未配置 GitHub Webhook Secret。"
	case errorCodeInvalidSignature:
		return "Webhook 签名校验失败。"
	case errorCodeReconcileUnavailable:
		return "仓库同步服务当前不可用。"
	case errorCodeServiceUnavailable:
		return "服务当前不可用，请稍后重试。"
	case errorCodeEncryptionUnavailable:
		return "加密主密钥不可用，无法保存敏感配置。"
	case errorCodeExternalRepoLimit:
		return fmt.Sprintf("外部公开仓库已达上限（%d 个）。", store.MaxExternalRepositories)
	case errorCodeGitHubFieldLocked:
		return "该字段已由环境变量设置，管理台不能覆盖；请修改部署配置后重启。"
	case errorCodeGitHubAppNotConfigured:
		return "尚未配置 GitHub App ID 与私钥，无法调用 GitHub API。"
	case errorCodeGitHubNoInstallation:
		return "本地尚无 Installation 记录。请先在 GitHub 安装 App，或等待 installation 事件到达。"
	case errorCodeReconcileInProgress:
		return "已有对账任务在进行中，请稍后再试。"
	case errorCodeAIFieldLocked:
		return "该字段已由环境变量设置，管理台不能覆盖；请修改部署配置后重启。"
	case errorCodeMethodNotAllowed:
		return "当前请求方法不受支持。"
	default:
		return "服务器暂时无法完成请求，请使用 request_id 排查。"
	}
}

package httpapi

import (
	"errors"
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
			"error_code", errorCodeInternal,
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
		w.Header().Set("Retry-After", "12")
	}
	if errorCode == errorCodeInternal && details == nil {
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
	case "webhook_not_configured":
		return "尚未配置 GitHub Webhook Secret。"
	case "invalid_signature":
		return "Webhook 签名校验失败。"
	case "reconcile_unavailable":
		return "仓库同步服务当前不可用。"
	case "encryption_unavailable":
		return "加密主密钥不可用，无法保存敏感配置。"
	default:
		return "服务器暂时无法完成请求，请使用 request_id 排查。"
	}
}

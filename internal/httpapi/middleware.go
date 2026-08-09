package httpapi

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"runtime/debug"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/oklog/ulid/v2"
)

type contextKey string

const (
	requestIDContextKey contextKey = "request_id"
	remoteIPContextKey  contextKey = "remote_ip"
	sessionContextKey   contextKey = "session"
)

func (s *server) requestIDMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requestID := ulid.Make().String()
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, requestID)))
	})
}

func (s *server) realIPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteIP := clientIPFromRemoteAddr(r.RemoteAddr)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), remoteIPContextKey, remoteIP)))
	})
}

func (s *server) accessLogMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		startedAt := time.Now()
		wrapped := &statusResponseWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		// 页面与 API 的逐请求访问日志默认不刷屏；排查时将 logging.level 设为 debug。
		s.dependencies.Logger.Debug(
			"http request",
			"request_id", requestIDFromContext(r.Context()),
			"method", r.Method,
			"path", r.URL.Path,
			"remote_ip", remoteIPFromContext(r.Context()),
			"http_status", wrapped.status,
			"duration_ms", time.Since(startedAt).Milliseconds(),
			"user_agent", r.UserAgent(),
		)
	})
}

func (s *server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			recovered := recover()
			if recovered == nil {
				return
			}
			// panic 值常携带关键线索（如空指针解引用对象），必须进 Error 日志；
			// 完整堆栈较大，放 Debug 级别避免刷屏，排查时把 logging.level 调成 debug 即可复现。
			s.dependencies.Logger.Error(
				"http panic recovered",
				"request_id", requestIDFromContext(r.Context()),
				"error_code", errorCodeInternal,
				"panic", fmt.Sprintf("%v", recovered),
			)
			s.dependencies.Logger.Debug(
				"http panic stack",
				"request_id", requestIDFromContext(r.Context()),
				"stack", string(debug.Stack()),
			)
			s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) authenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// 优先 Session Cookie（管理台）。
		if cookie, err := r.Cookie(SessionCookieName); err == nil {
			session, err := s.dependencies.SessionService.Authenticate(r.Context(), cookie.Value)
			if err != nil {
				s.writeMappedError(w, r, err)
				return
			}
			session, err = s.dependencies.SessionService.Touch(r.Context(), session)
			if err != nil {
				s.writeMappedError(w, r, err)
				return
			}
			ctx := context.WithValue(r.Context(), sessionContextKey, session)
			next.ServeHTTP(w, r.WithContext(ctx))
			return
		}
		// 无 Session Cookie 时尝试 OAuth Bearer（Agent 只读访问）。
		if token, ok := bearerToken(r); ok {
			audience := s.siteOrigin(r) + "/api/v1"
			if clientID, err := s.oauthValidateToken(token, audience); err == nil {
				ctx := context.WithValue(r.Context(), agentClientContextKey, clientID)
				next.ServeHTTP(w, r.WithContext(ctx))
				return
			}
		}
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
	})
}

func (s *server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Agent Bearer 认证已自带请求方身份，无需双提交 CSRF。
		if _, ok := agentClientIDFromContext(r.Context()); ok {
			next.ServeHTTP(w, r)
			return
		}
		session, ok := sessionFromContext(r.Context())
		if !ok {
			s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
			return
		}
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil {
			s.logCSRFFailure(r)
			s.writeAPIError(w, r, http.StatusForbidden, errorCodeCSRFFailed, nil)
			return
		}
		if err := s.dependencies.CSRF.Validate(cookie.Value, r.Header.Get(CSRFHeaderName), session.CSRFHash); err != nil {
			s.logCSRFFailure(r)
			s.writeMappedError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// logCSRFFailure 记录 CSRF 校验失败：写请求被拒通常是安全事件（跨站请求伪造尝试），
// 留痕 request_id 与来源 IP 便于审计，不暴露令牌内容。
func (s *server) logCSRFFailure(r *http.Request) {
	s.dependencies.Logger.Warn(
		"csrf validation failed",
		"request_id", requestIDFromContext(r.Context()),
		"remote_ip", remoteIPFromContext(r.Context()),
		"method", r.Method,
		"path", r.URL.Path,
		"error_code", errorCodeCSRFFailed,
	)
}

func requestIDFromContext(ctx context.Context) string {
	value, _ := ctx.Value(requestIDContextKey).(string)
	return value
}

func remoteIPFromContext(ctx context.Context) string {
	value, _ := ctx.Value(remoteIPContextKey).(string)
	return value
}

func sessionFromContext(ctx context.Context) (auth.Session, bool) {
	value, ok := ctx.Value(sessionContextKey).(auth.Session)
	return value, ok
}

func clientIPFromRemoteAddr(remoteAddr string) string {
	remoteAddr = strings.TrimSpace(remoteAddr)
	host, _, err := net.SplitHostPort(remoteAddr)
	if err == nil {
		return strings.Trim(host, "[]")
	}
	return strings.Trim(remoteAddr, "[]")
}

type statusResponseWriter struct {
	http.ResponseWriter
	status      int
	wroteHeader bool
}

func (w *statusResponseWriter) WriteHeader(status int) {
	if w.wroteHeader {
		return
	}
	w.status = status
	w.wroteHeader = true
	w.ResponseWriter.WriteHeader(status)
}

func (w *statusResponseWriter) Write(body []byte) (int, error) {
	if !w.wroteHeader {
		w.WriteHeader(http.StatusOK)
	}
	return w.ResponseWriter.Write(body)
}

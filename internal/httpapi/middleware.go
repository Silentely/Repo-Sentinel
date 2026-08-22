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
		// 上游（反代）已带 X-Request-ID 时沿用，便于跨组件按同一 ID 串联日志；
		// 未带或格式非法（过长/不可打印）时本地生成。
		requestID := strings.TrimSpace(r.Header.Get("X-Request-ID"))
		if !validRequestID(requestID) {
			requestID = ulid.Make().String()
		}
		w.Header().Set("X-Request-ID", requestID)
		next.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), requestIDContextKey, requestID)))
	})
}

// validRequestID 校验上游请求 ID：非空、长度受限、仅可打印 ASCII，
// 避免把不可信头部原样回写响应头（CRLF 注入防护）。
func validRequestID(id string) bool {
	if id == "" || len(id) > 128 {
		return false
	}
	for _, c := range id {
		if c < 0x21 || c > 0x7e {
			return false
		}
	}
	return true
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
		// 追踪响应是否已写出：handler 已写头/部分 body 后 panic 时，再补写 500 会产生
		// superfluous WriteHeader 与截断脏响应，此时只记日志、让连接自然结束。
		wrote := false
		tracker := &responseWriteTracker{ResponseWriter: w, wrote: &wrote}
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
			if !wrote {
				s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
			}
		}()
		next.ServeHTTP(tracker, r)
	})
}

// responseWriteTracker 记录响应是否已写出（WriteHeader 或隐式 200 的 Write）。
type responseWriteTracker struct {
	http.ResponseWriter
	wrote *bool
}

func (t *responseWriteTracker) WriteHeader(code int) {
	*t.wrote = true
	t.ResponseWriter.WriteHeader(code)
}

// storeGuardMiddleware 统一保护依赖 Store 的受保护路由：Store 未装配（理论上仅在
// 非正常启动路径出现）时返回 503 而非 nil 解引用 panic。
// 各 handler 的分散守卫保留（既有防御），此处兜底未覆盖的分支。
func (s *server) storeGuardMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.dependencies.Store == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (t *responseWriteTracker) Write(b []byte) (int, error) {
	*t.wrote = true
	return t.ResponseWriter.Write(b)
}

// Unwrap 供 http.NewResponseController 穿透包装层。
func (t *responseWriteTracker) Unwrap() http.ResponseWriter {
	return t.ResponseWriter
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
			origin := s.siteOrigin(r)
			if clientID, err := s.oauthValidateToken(token, origin+"/api/v1", origin); err == nil {
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

// Unwrap 供 http.NewResponseController 穿透包装层（SetWriteDeadline/Flush 依赖
// Unwrap 链逐层解包，缺失会让 controller 静默失效）。
func (w *statusResponseWriter) Unwrap() http.ResponseWriter {
	return w.ResponseWriter
}

// Flush 透传底层 Flusher（流式响应场景），无 Flusher 时静默忽略。
func (w *statusResponseWriter) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

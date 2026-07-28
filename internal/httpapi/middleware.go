package httpapi

import (
	"context"
	"net"
	"net/http"
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
		)
	})
}

func (s *server) recoveryMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if recover() == nil {
				return
			}
			s.dependencies.Logger.Error(
				"http panic recovered",
				"request_id", requestIDFromContext(r.Context()),
				"error_code", errorCodeInternal,
			)
			s.writeAPIError(w, r, http.StatusInternalServerError, errorCodeInternal, nil)
		}()
		next.ServeHTTP(w, r)
	})
}

func (s *server) authenticationMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cookie, err := r.Cookie(SessionCookieName)
		if err != nil {
			s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
			return
		}
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
	})
}

func (s *server) csrfMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, ok := sessionFromContext(r.Context())
		if !ok {
			s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
			return
		}
		cookie, err := r.Cookie(CSRFCookieName)
		if err != nil {
			s.writeAPIError(w, r, http.StatusForbidden, errorCodeCSRFFailed, nil)
			return
		}
		if err := s.dependencies.CSRF.Validate(cookie.Value, r.Header.Get(CSRFHeaderName), session.CSRFHash); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		next.ServeHTTP(w, r)
	})
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

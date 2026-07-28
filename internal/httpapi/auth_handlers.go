package httpapi

import (
	"errors"
	"net/http"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/auth"
)

type loginRequest struct {
	Username string `json:"username"`
	Password string `json:"password"`
}

type changePasswordRequest struct {
	CurrentPassword string `json:"current_password"`
	NewPassword     string `json:"new_password"`
}

type adminResponse struct {
	ID       string `json:"id"`
	Username string `json:"username"`
}

type sessionResponse struct {
	ID        string    `json:"id"`
	ExpiresAt time.Time `json:"expires_at"`
}

type authenticationResponse struct {
	Admin   adminResponse   `json:"admin"`
	Session sessionResponse `json:"session"`
}

func (s *server) handleLogin(w http.ResponseWriter, r *http.Request) {
	var request loginRequest
	if !s.decodeRequestJSON(w, r, &request) {
		return
	}
	remoteIP := remoteIPFromContext(r.Context())
	requestID := requestIDFromContext(r.Context())
	if !s.dependencies.LoginLimiter.Allow(remoteIP) {
		s.dependencies.Logger.Warn(
			"login rate limited",
			"request_id", requestID,
			"remote_ip", remoteIP,
			"error_code", errorCodeRateLimited,
		)
		s.writeAPIError(w, r, http.StatusTooManyRequests, errorCodeRateLimited, nil)
		return
	}
	admin, err := s.dependencies.AdminService.Authenticate(r.Context(), request.Username, request.Password)
	if err != nil {
		// 凭据错误记 Warn；其它错误交 writeMappedError 记 Error，避免重复。
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.dependencies.Logger.Warn(
				"login failed",
				"request_id", requestID,
				"remote_ip", remoteIP,
				"error_code", errorCodeInvalidCredentials,
			)
		}
		s.writeMappedError(w, r, err)
		return
	}
	created, err := s.dependencies.SessionService.Create(r.Context(), admin.ID, remoteIP, r.UserAgent())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	s.dependencies.Logger.Info(
		"login success",
		"request_id", requestID,
		"admin_id", admin.ID,
		"remote_ip", remoteIP,
		"session_id", created.Session.ID,
	)
	s.setAuthCookies(w, created)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, newAuthenticationResponse(admin, created.Session))
}

func (s *server) handleSession(w http.ResponseWriter, r *http.Request) {
	session, ok := sessionFromContext(r.Context())
	if !ok {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
		return
	}
	account, err := s.dependencies.AdminStore.Get(r.Context(), session.AdminID)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, newAuthenticationResponse(
		auth.Admin{ID: account.ID, Username: account.Username},
		session,
	))
}

func (s *server) handleLogout(w http.ResponseWriter, r *http.Request) {
	var request struct{}
	if !s.decodeRequestJSON(w, r, &request) {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
		return
	}
	if err := s.dependencies.SessionService.Logout(r.Context(), session.ID); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	s.dependencies.Logger.Info(
		"logout success",
		"request_id", requestIDFromContext(r.Context()),
		"admin_id", session.AdminID,
		"session_id", session.ID,
	)
	s.clearAuthCookies(w)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"logged_out": true})
}

func (s *server) handleChangePassword(w http.ResponseWriter, r *http.Request) {
	var request changePasswordRequest
	if !s.decodeRequestJSON(w, r, &request) {
		return
	}
	session, ok := sessionFromContext(r.Context())
	if !ok {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeUnauthorized, nil)
		return
	}
	if err := s.dependencies.AdminService.ChangePassword(
		r.Context(),
		session.AdminID,
		session.ID,
		request.CurrentPassword,
		request.NewPassword,
	); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]bool{"changed": true})
}

func newAuthenticationResponse(admin auth.Admin, session auth.Session) authenticationResponse {
	return authenticationResponse{
		Admin: adminResponse{ID: admin.ID, Username: admin.Username},
		Session: sessionResponse{
			ID:        session.ID,
			ExpiresAt: session.ExpiresAt.UTC(),
		},
	}
}

func (s *server) setAuthCookies(w http.ResponseWriter, created auth.CreatedSession) {
	maxAge := int(created.Session.ExpiresAt.Sub(created.Session.CreatedAt).Seconds())
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    created.Token,
		Path:     "/",
		Expires:  created.Session.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    created.CSRFToken,
		Path:     "/",
		Expires:  created.Session.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   s.secureCookies,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearAuthCookies(w http.ResponseWriter) {
	expiredAt := time.Unix(1, 0).UTC()
	for _, cookie := range []*http.Cookie{
		{
			Name:     SessionCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   s.secureCookies,
			SameSite: http.SameSiteLaxMode,
		},
		{
			Name:     CSRFCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			HttpOnly: false,
			Secure:   s.secureCookies,
			SameSite: http.SameSiteLaxMode,
		},
	} {
		http.SetCookie(w, cookie)
	}
}

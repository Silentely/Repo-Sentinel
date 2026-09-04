package httpapi

import (
	"errors"
	"net/http"
	"strings"
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
	request.Username = strings.TrimSpace(request.Username)
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
	if s.loginSem != nil {
		select {
		case s.loginSem <- struct{}{}:
			defer func() { <-s.loginSem }()
		case <-time.After(1 * time.Second):
			s.dependencies.Logger.Warn(
				"login concurrency limit reached",
				"request_id", requestID,
				"remote_ip", remoteIP,
				"username", request.Username,
				"error_code", errorCodeRateLimited,
			)
			s.writeAPIError(w, r, http.StatusTooManyRequests, errorCodeRateLimited, nil)
			return
		}
	}

	admin, err := s.dependencies.AdminService.Authenticate(r.Context(), request.Username, request.Password)
	if err != nil {
		s.dependencies.LoginLimiter.RecordFailure(request.Username)
		if delay := s.dependencies.LoginLimiter.DelayFor(request.Username); delay > 0 {
			select {
			case <-time.After(delay):
			case <-r.Context().Done():
			}
		}
		// 凭据错误记 Warn；其它错误交 writeMappedError 记 Error，避免重复。
		// username 用于审计暴力尝试的账号维度，绝不记录密码。
		if errors.Is(err, auth.ErrInvalidCredentials) {
			s.dependencies.Logger.Warn(
				"login failed",
				"request_id", requestID,
				"remote_ip", remoteIP,
				"username", request.Username,
				"error_code", errorCodeInvalidCredentials,
			)
		}
		s.writeMappedError(w, r, err)
		return
	}
	s.dependencies.LoginLimiter.RecordSuccess(request.Username)

	totpEnabled, _, err := auth.LoadTOTPConfig(r.Context(), s.dependencies.Store, s.dependencies.KeyRing)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if totpEnabled {
		ticket := s.getTOTPTickets().CreateTicket(admin.ID, admin.Username, remoteIP)
		s.dependencies.Logger.Info(
			"login requires 2fa",
			"request_id", requestID,
			"admin_id", admin.ID,
			"remote_ip", remoteIP,
		)
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, http.StatusOK, map[string]any{
			"requires_2fa": true,
			"ticket":       ticket,
		})
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
		"user_agent", r.UserAgent(),
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
	// 密码是对认证服务不透明的凭据；仅拒绝全空白输入，保留合法的首尾空格。
	if strings.TrimSpace(request.CurrentPassword) == "" || strings.TrimSpace(request.NewPassword) == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
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
	// 改密后撤销其它会话：密码已更换，旧会话继续存活是安全漏洞（前端文案
	// 「其它会话将失效」与实现此前不符）。撤销失败不阻断改密结果，留痕即可。
	if revoked, err := s.dependencies.SessionService.RevokeOthers(r.Context(), session.AdminID, session.ID); err != nil && s.dependencies.Logger != nil {
		s.dependencies.Logger.Warn("revoke other sessions failed after password change",
			"request_id", requestIDFromContext(r.Context()),
			"error_code", "session_revoke_failed",
			"error", err.Error())
	} else if s.dependencies.Logger != nil {
		s.dependencies.Logger.Info("other sessions revoked after password change",
			"request_id", requestIDFromContext(r.Context()),
			"revoked", revoked)
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
	secure := s.cookiesSecure()
	http.SetCookie(w, &http.Cookie{
		Name:     SessionCookieName,
		Value:    created.Token,
		Path:     "/",
		Expires:  created.Session.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
	http.SetCookie(w, &http.Cookie{
		Name:     CSRFCookieName,
		Value:    created.CSRFToken,
		Path:     "/",
		Expires:  created.Session.ExpiresAt,
		MaxAge:   maxAge,
		HttpOnly: false,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func (s *server) clearAuthCookies(w http.ResponseWriter) {
	expiredAt := time.Unix(1, 0).UTC()
	secure := s.cookiesSecure()
	for _, cookie := range []*http.Cookie{
		{
			Name:     SessionCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			HttpOnly: true,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		},
		{
			Name:     CSRFCookieName,
			Path:     "/",
			Expires:  expiredAt,
			MaxAge:   -1,
			HttpOnly: false,
			Secure:   secure,
			SameSite: http.SameSiteLaxMode,
		},
	} {
		http.SetCookie(w, cookie)
	}
}

// cookiesSecure 优先使用运行时 Public Base URL（可被管理台热更新），否则回退启动时配置。
func (s *server) cookiesSecure() bool {
	if s.dependencies.GitHubRuntime != nil {
		if base := strings.TrimSpace(s.dependencies.GitHubRuntime.Snapshot().PublicBaseURL); base != "" {
			return usesSecureCookies(base)
		}
	}
	return s.secureCookies
}

type login2FARequest struct {
	Ticket   string `json:"ticket"`
	Passcode string `json:"passcode"`
}

func (s *server) handleLogin2FA(w http.ResponseWriter, r *http.Request) {
	var request login2FARequest
	if !s.decodeRequestJSON(w, r, &request) {
		return
	}
	request.Ticket = strings.TrimSpace(request.Ticket)
	request.Passcode = strings.TrimSpace(request.Passcode)
	remoteIP := remoteIPFromContext(r.Context())
	requestID := requestIDFromContext(r.Context())

	ticketMgr := s.getTOTPTickets()
	ticket, ok := ticketMgr.GetTicket(request.Ticket, remoteIP)
	if !ok {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, map[string]any{
			"reason": "ticket_expired_or_invalid",
		})
		return
	}

	totpEnabled, secret, err := auth.LoadTOTPConfig(r.Context(), s.dependencies.Store, s.dependencies.KeyRing)
	if err != nil || !totpEnabled || secret == "" {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, nil)
		return
	}

	if !auth.ValidateTOTP(secret, request.Passcode, time.Now().UTC()) {
		failures := ticketMgr.RecordFailure(request.Ticket)
		s.dependencies.Logger.Warn(
			"login 2fa failed",
			"request_id", requestID,
			"remote_ip", remoteIP,
			"username", ticket.Username,
			"failures", failures,
		)
		remaining := 3 - failures
		if remaining < 0 {
			remaining = 0
		}
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, map[string]any{
			"remaining_attempts": remaining,
		})
		return
	}

	// 校验与消费之间允许并发请求进入；只有原子消费成功者才能创建 Session，
	// 确保临时票据真正一次性使用。
	if !ticketMgr.ConsumeTicket(request.Ticket) {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, map[string]any{
			"reason": "ticket_expired_or_invalid",
		})
		return
	}

	created, err := s.dependencies.SessionService.Create(r.Context(), ticket.AdminID, remoteIP, r.UserAgent())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	s.dependencies.Logger.Info(
		"login 2fa success",
		"request_id", requestID,
		"admin_id", ticket.AdminID,
		"remote_ip", remoteIP,
		"user_agent", r.UserAgent(),
		"session_id", created.Session.ID,
	)
	s.setAuthCookies(w, created)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, newAuthenticationResponse(auth.Admin{ID: ticket.AdminID, Username: ticket.Username}, created.Session))
}

func (s *server) handleGet2FA(w http.ResponseWriter, r *http.Request) {
	enabled, _, err := auth.LoadTOTPConfig(r.Context(), s.dependencies.Store, s.dependencies.KeyRing)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"enabled": enabled})
}

func (s *server) handleSetup2FA(w http.ResponseWriter, r *http.Request) {
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
	secret, err := auth.GenerateTOTPSecret()
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	otpURL := auth.GenerateOTPAuthURL(account.Username, secret, "RepoSentinel")
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{
		"secret":      secret,
		"otpauth_url": otpURL,
	})
}

type enable2FARequest struct {
	Secret   string `json:"secret"`
	Passcode string `json:"passcode"`
}

func (s *server) handleEnable2FA(w http.ResponseWriter, r *http.Request) {
	var req enable2FARequest
	if !s.decodeRequestJSON(w, r, &req) {
		return
	}
	req.Secret = strings.TrimSpace(req.Secret)
	req.Passcode = strings.TrimSpace(req.Passcode)
	if req.Secret == "" || !auth.ValidateTOTP(req.Secret, req.Passcode, time.Now().UTC()) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{
			"field":  "passcode",
			"reason": "invalid_passcode",
		})
		return
	}
	if err := auth.SaveTOTPConfig(r.Context(), s.dependencies.Store, s.dependencies.KeyRing, true, req.Secret); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	session, _ := sessionFromContext(r.Context())
	s.dependencies.Logger.Info(
		"totp 2fa enabled",
		"request_id", requestIDFromContext(r.Context()),
		"admin_id", session.AdminID,
	)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"enabled": true})
}

type disable2FARequest struct {
	CurrentPassword string `json:"current_password"`
}

func (s *server) handleDisable2FA(w http.ResponseWriter, r *http.Request) {
	var req disable2FARequest
	if !s.decodeRequestJSON(w, r, &req) {
		return
	}
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
	if _, err := s.dependencies.AdminService.Authenticate(r.Context(), account.Username, req.CurrentPassword); err != nil {
		s.writeAPIError(w, r, http.StatusUnauthorized, errorCodeInvalidCredentials, nil)
		return
	}
	if err := auth.DisableTOTP(r.Context(), s.dependencies.Store); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	s.dependencies.Logger.Info(
		"totp 2fa disabled",
		"request_id", requestIDFromContext(r.Context()),
		"admin_id", session.AdminID,
	)
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]any{"enabled": false})
}

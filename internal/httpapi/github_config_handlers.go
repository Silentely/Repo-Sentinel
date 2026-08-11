package httpapi

import (
	"net/http"
	"net/url"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
)

type githubConfigResponse struct {
	AppID                   int64  `json:"app_id"`
	ClientID                string `json:"client_id"`
	PrivateKeyPath          string `json:"private_key_path"`
	PublicBaseURL           string `json:"public_base_url"`
	WebhookPath             string `json:"webhook_path"`
	WebhookURL              string `json:"webhook_url"`
	AppIDConfigured         bool   `json:"app_id_configured"`
	ClientIDConfigured      bool   `json:"client_id_configured"`
	PrivateKeyConfigured    bool   `json:"private_key_configured"`
	WebhookSecretConfigured bool   `json:"webhook_secret_configured"`
	ExternalPATConfigured   bool   `json:"external_pat_configured"`
	AppIDSource             string `json:"app_id_source"`
	ClientIDSource          string `json:"client_id_source"`
	PrivateKeySource        string `json:"private_key_source"`
	WebhookSecretSource     string `json:"webhook_secret_source"`
	PublicBaseURLSource     string `json:"public_base_url_source"`
	AppIDLocked             bool   `json:"app_id_locked"`
	ClientIDLocked          bool   `json:"client_id_locked"`
	PrivateKeyLocked        bool   `json:"private_key_locked"`
	WebhookSecretLocked     bool   `json:"webhook_secret_locked"`
	PublicBaseURLLocked     bool   `json:"public_base_url_locked"`
	CanEditInUI             bool   `json:"can_edit_in_ui"`
	Note                    string `json:"note"`
}

type githubConfigPutRequest struct {
	AppID          *int64  `json:"app_id"`
	ClientID       *string `json:"client_id"`
	PrivateKeyPath *string `json:"private_key_path"`
	PrivateKeyPEM  *string `json:"private_key_pem"`
	WebhookSecret  *string `json:"webhook_secret"`
	PublicBaseURL  *string `json:"public_base_url"`
	// ClearPrivateKey / ClearWebhookSecret 显式清除数据库中的值（仅当字段未被 env 锁定）。
	ClearPrivateKey    bool `json:"clear_private_key"`
	ClearWebhookSecret bool `json:"clear_webhook_secret"`
}

// rejectEnvLockedField 字段被环境变量锁定时拒绝写入并返回 true。
// 六个可变字段共用同一锁定语义，避免逐个复制判断。
func (s *server) rejectEnvLockedField(w http.ResponseWriter, r *http.Request, source, field string) bool {
	if source == "env" {
		s.writeAPIError(w, r, http.StatusConflict, "github_field_locked", map[string]any{"field": field})
		return true
	}
	return false
}

func (s *server) handleGetGitHubConfig(w http.ResponseWriter, r *http.Request) {
	writeJSON(w, http.StatusOK, s.githubConfigView(r))
}

func (s *server) handlePutGitHubConfig(w http.ResponseWriter, r *http.Request) {
	if s.dependencies.GitHubRuntime == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	var body githubConfigPutRequest
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}

	rt := s.dependencies.GitHubRuntime
	snap := rt.Snapshot()
	stored, err := githubx.LoadStoredRuntime(r.Context(), s.dependencies.Store)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	// 仅更新「未被环境变量锁定」的字段。
	if body.AppID != nil {
		if s.rejectEnvLockedField(w, r, snap.AppIDSource, "app_id") {
			return
		}
		if *body.AppID < 0 {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "app_id"})
			return
		}
		stored.AppID = *body.AppID
	}
	if body.ClientID != nil {
		if s.rejectEnvLockedField(w, r, snap.ClientIDSource, "client_id") {
			return
		}
		stored.ClientID = strings.TrimSpace(*body.ClientID)
	}
	if body.PublicBaseURL != nil {
		if s.rejectEnvLockedField(w, r, snap.PublicBaseURLSource, "public_base_url") {
			return
		}
		base := strings.TrimSpace(*body.PublicBaseURL)
		if base != "" && !validOptionalPublicBaseURL(base) {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "public_base_url"})
			return
		}
		stored.PublicBaseURL = base
	}

	if body.ClearPrivateKey {
		if s.rejectEnvLockedField(w, r, snap.PrivateKeySource, "private_key") {
			return
		}
		stored.PrivateKeyPath = ""
		stored.PrivateKeyPEMEnvelope = ""
	}
	if body.PrivateKeyPath != nil || body.PrivateKeyPEM != nil {
		if s.rejectEnvLockedField(w, r, snap.PrivateKeySource, "private_key") {
			return
		}
		if body.PrivateKeyPEM != nil && strings.TrimSpace(*body.PrivateKeyPEM) != "" {
			pemText := strings.TrimSpace(*body.PrivateKeyPEM)
			if err := githubx.ValidatePrivateKeyPEM(pemText); err != nil {
				s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{
					"field": "private_key_pem", "reason": "invalid_pem",
				})
				return
			}
			if s.dependencies.KeyRing == nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, "encryption_unavailable", nil)
				return
			}
			env, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, pemText)
			if err != nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, "encryption_unavailable", nil)
				return
			}
			stored.PrivateKeyPEMEnvelope = env
			stored.PrivateKeyPath = ""
		} else if body.PrivateKeyPath != nil {
			stored.PrivateKeyPath = strings.TrimSpace(*body.PrivateKeyPath)
			if stored.PrivateKeyPath != "" {
				stored.PrivateKeyPEMEnvelope = ""
			}
		}
	}

	if body.ClearWebhookSecret {
		if s.rejectEnvLockedField(w, r, snap.WebhookSecretSource, "webhook_secret") {
			return
		}
		stored.WebhookSecretEnvelope = ""
	}
	if body.WebhookSecret != nil && strings.TrimSpace(*body.WebhookSecret) != "" {
		if s.rejectEnvLockedField(w, r, snap.WebhookSecretSource, "webhook_secret") {
			return
		}
		if s.dependencies.KeyRing == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, "encryption_unavailable", nil)
			return
		}
		env, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, strings.TrimSpace(*body.WebhookSecret))
		if err != nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, "encryption_unavailable", nil)
			return
		}
		stored.WebhookSecretEnvelope = env
	}

	if err := githubx.SaveStoredRuntime(r.Context(), s.dependencies.Store, stored); err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	// 从 env 基线重建，再合并 DB，保证 env 始终优先。
	base := githubx.RuntimeFromEnv(
		s.dependencies.Config.GitHub.AppID,
		s.dependencies.Config.GitHub.ClientID,
		s.dependencies.Config.GitHub.PrivateKeyPath,
		s.dependencies.Config.GitHub.WebhookSecret.Reveal(),
		s.dependencies.Config.GitHub.WebhookPreviousSecret.Reveal(),
		s.dependencies.Config.GitHub.ExternalPAT.Reveal(),
		s.dependencies.Config.HTTP.PublicBaseURL,
		rt.Client,
	)
	rt.Replace(base)
	if err := githubx.MergeFromStore(r.Context(), s.dependencies.Store, s.dependencies.KeyRing, rt); err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	s.dependencies.Logger.Info(
		"github config updated",
		"request_id", requestIDFromContext(r.Context()),
		"app_id_configured", rt.Snapshot().AppID > 0,
		"webhook_secret_configured", strings.TrimSpace(rt.Snapshot().WebhookSecret) != "",
	)
	writeJSON(w, http.StatusOK, s.githubConfigView(r))
}

func (s *server) githubConfigView(r *http.Request) githubConfigResponse {
	note := "环境变量优先；未用环境变量设置的字段可在此保存到数据库（私钥与 Webhook Secret 加密存储）。保存后立即生效，无需重启。"
	out := githubConfigResponse{
		WebhookPath: githubx.WebhookPath,
		Note:        note,
		CanEditInUI: s.dependencies.GitHubRuntime != nil,
	}
	if s.dependencies.GitHubRuntime == nil {
		out.PublicBaseURL = s.dependencies.Config.HTTP.PublicBaseURL
		out.WebhookURL = joinWebhookURL(out.PublicBaseURL, out.WebhookPath, r)
		return out
	}
	snap := s.dependencies.GitHubRuntime.Snapshot()
	appID, clientID, privateKey, webhook, _, externalPAT, base, path := s.dependencies.GitHubRuntime.StatusFlags()
	out.AppID = snap.AppID
	out.ClientID = snap.ClientID
	out.PrivateKeyPath = snap.PrivateKeyPath
	out.PublicBaseURL = base
	if out.PublicBaseURL == "" {
		out.PublicBaseURL = snap.PublicBaseURL
	}
	out.WebhookPath = path
	out.WebhookURL = joinWebhookURL(out.PublicBaseURL, path, r)
	out.AppIDConfigured = appID
	out.ClientIDConfigured = clientID
	out.PrivateKeyConfigured = privateKey
	out.WebhookSecretConfigured = webhook
	out.ExternalPATConfigured = externalPAT
	out.AppIDSource = snap.AppIDSource
	out.ClientIDSource = snap.ClientIDSource
	out.PrivateKeySource = snap.PrivateKeySource
	out.WebhookSecretSource = snap.WebhookSecretSource
	out.PublicBaseURLSource = snap.PublicBaseURLSource
	out.AppIDLocked = snap.AppIDSource == "env"
	out.ClientIDLocked = snap.ClientIDSource == "env"
	out.PrivateKeyLocked = snap.PrivateKeySource == "env"
	out.WebhookSecretLocked = snap.WebhookSecretSource == "env"
	out.PublicBaseURLLocked = snap.PublicBaseURLSource == "env"
	return out
}

func joinWebhookURL(publicBase, path string, r *http.Request) string {
	path = strings.TrimSpace(path)
	if path == "" {
		path = githubx.WebhookPath
	}
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}
	base := strings.TrimRight(strings.TrimSpace(publicBase), "/")
	if base == "" && r != nil {
		scheme := "http"
		if r.TLS != nil {
			scheme = "https"
		}
		if proto := r.Header.Get("X-Forwarded-Proto"); proto != "" {
			scheme = proto
		}
		host := r.Host
		if host != "" {
			base = scheme + "://" + host
		}
	}
	if base == "" {
		return path
	}
	return base + path
}

func validOptionalPublicBaseURL(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	if err != nil || parsed.Scheme == "" || parsed.Host == "" {
		return false
	}
	switch strings.ToLower(parsed.Scheme) {
	case "https":
		return true
	case "http":
		host := strings.ToLower(parsed.Hostname())
		return host == "localhost" || host == "127.0.0.1" || host == "::1"
	default:
		return false
	}
}

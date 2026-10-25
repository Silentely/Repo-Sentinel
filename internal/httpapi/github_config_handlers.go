package httpapi

import (
	"encoding/json"
	"fmt"
	"io"
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
		s.writeAPIError(w, r, http.StatusConflict, errorCodeGitHubFieldLocked, map[string]any{"field": field})
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
		if body.PrivateKeyPEM != nil && strings.TrimSpace(*body.PrivateKeyPEM) != "" {
			if s.rejectEnvLockedField(w, r, snap.PrivateKeySource, "private_key") {
				return
			}
			pemText := strings.TrimSpace(*body.PrivateKeyPEM)
			if err := githubx.ValidatePrivateKeyPEM(pemText); err != nil {
				s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{
					"field": "private_key_pem", "reason": "invalid_pem",
				})
				return
			}
			if s.dependencies.KeyRing == nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
				return
			}
			env, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, pemText)
			if err != nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
				return
			}
			stored.PrivateKeyPEMEnvelope = env
			stored.PrivateKeyPath = ""
		} else if body.PrivateKeyPath != nil {
			if s.rejectEnvLockedField(w, r, snap.PrivateKeySource, "private_key") {
				return
			}
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
	if body.WebhookSecret != nil {
		secret := strings.TrimSpace(*body.WebhookSecret)
		if secret != "" {
			if s.rejectEnvLockedField(w, r, snap.WebhookSecretSource, "webhook_secret") {
				return
			}
			if s.dependencies.KeyRing == nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
				return
			}
			env, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, secret)
			if err != nil {
				s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
				return
			}
			stored.WebhookSecretEnvelope = env
		}
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

	// GitHub 配置更新后主动清空探针缓存，让下次健康检查立即采用新凭证进行探测
	s.ghProbeMu.Lock()
	s.ghProbeCache = nil
	s.ghProbeMu.Unlock()

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
	// base 即 snap.PublicBaseURL（见 StatusFlags），无需再回退。
	out.PublicBaseURL = base
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
		// X-Forwarded-Proto 归一化：按逗号取首段、大小写归一，且只接受 http/https。
		// 原样透传会得出 javascript://host/path 这类畸形值（scheme 未被限定）。
		if proto := strings.TrimSpace(r.Header.Get("X-Forwarded-Proto")); proto != "" {
			if idx := strings.IndexByte(proto, ','); idx >= 0 {
				proto = strings.TrimSpace(proto[:idx])
			}
			switch strings.ToLower(proto) {
			case "https":
				scheme = "https"
			case "http":
				scheme = "http"
			}
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

func (s *server) handleGetGitHubManifest(w http.ResponseWriter, r *http.Request) {
	baseURL := ""
	if s.dependencies.GitHubRuntime != nil {
		snap := s.dependencies.GitHubRuntime.Snapshot()
		baseURL = strings.TrimRight(snap.PublicBaseURL, "/")
	}
	if baseURL == "" {
		host := r.Host
		scheme := "https"
		if r.TLS == nil && !strings.HasPrefix(host, "localhost") && !strings.HasPrefix(host, "127.0.0.1") {
			scheme = "http"
		}
		baseURL = fmt.Sprintf("%s://%s", scheme, host)
	}

	manifest := map[string]any{
		"name": "Repo-Sentinel",
		"url":  baseURL,
		"hook_attributes": map[string]any{
			"url": fmt.Sprintf("%s/webhooks/github", baseURL),
		},
		"redirect_url": fmt.Sprintf("%s/github/manifest-callback", baseURL),
		"public":       false,
		"default_permissions": map[string]string{
			"issues":        "write",
			"pull_requests": "write", "contents": "read",
			"actions":              "read",
			"metadata":             "read",
			"security_events":      "read",
			"vulnerability_alerts": "read",
		},
		"default_events": []string{
			"issues", "pull_request", "workflow_run", "release", "push",
			"dependabot_alert", "code_scanning_alert", "secret_scanning_alert",
		},
	}
	writeJSON(w, http.StatusOK, manifest)
}

type gitHubManifestExchangeRequest struct {
	Code string `json:"code"`
}

func (s *server) handleExchangeGitHubManifest(w http.ResponseWriter, r *http.Request) {
	var req gitHubManifestExchangeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil || strings.TrimSpace(req.Code) == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "code"})
		return
	}
	if s.dependencies.GitHubRuntime == nil || s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeServiceUnavailable, nil)
		return
	}
	if s.dependencies.KeyRing == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
		return
	}

	convURL := "https://api.github.com/app-manifests/" + url.PathEscape(strings.TrimSpace(req.Code)) + "/conversions"
	ghReq, err := http.NewRequestWithContext(r.Context(), http.MethodPost, convURL, nil)
	if err != nil {
		s.writeAPIError(w, r, http.StatusInternalServerError, "github_manifest_failed", map[string]any{"message": err.Error()})
		return
	}
	ghReq.Header.Set("Accept", "application/vnd.github+json")
	ghReq.Header.Set("X-GitHub-Api-Version", "2022-11-28")

	resp, err := http.DefaultClient.Do(ghReq)
	if err != nil {
		s.writeAPIError(w, r, http.StatusBadGateway, "github_manifest_failed", map[string]any{"message": "连接 GitHub 兑换端点失败: " + err.Error()})
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusCreated && resp.StatusCode != http.StatusOK {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 8192))
		s.writeAPIError(w, r, http.StatusBadGateway, "github_manifest_failed", map[string]any{
			"message": fmt.Sprintf("GitHub 拒绝代码兑换 (HTTP %d): %s", resp.StatusCode, string(raw)),
		})
		return
	}

	var converted struct {
		ID            int64  `json:"id"`
		ClientID      string `json:"client_id"`
		WebhookSecret string `json:"webhook_secret"`
		PEM           string `json:"pem"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&converted); err != nil {
		s.writeAPIError(w, r, http.StatusBadGateway, "github_manifest_failed", map[string]any{"message": "解析 GitHub 兑换凭据失败"})
		return
	}

	stored, err := githubx.LoadStoredRuntime(r.Context(), s.dependencies.Store)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	if converted.ID > 0 {
		stored.AppID = converted.ID
	}
	if converted.ClientID != "" {
		stored.ClientID = converted.ClientID
	}
	if converted.WebhookSecret != "" {
		secEnv, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, converted.WebhookSecret)
		if err != nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
			return
		}
		stored.WebhookSecretEnvelope = secEnv
	}
	if converted.PEM != "" {
		pemEnv, err := githubx.EncryptSecret(r.Context(), s.dependencies.KeyRing, converted.PEM)
		if err != nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
			return
		}
		stored.PrivateKeyPEMEnvelope = pemEnv
	}

	if err := githubx.SaveStoredRuntime(r.Context(), s.dependencies.Store, stored); err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	_ = githubx.MergeFromStore(r.Context(), s.dependencies.Store, s.dependencies.KeyRing, s.dependencies.GitHubRuntime)

	writeJSON(w, http.StatusOK, map[string]any{
		"success":   true,
		"app_id":    converted.ID,
		"client_id": converted.ClientID,
	})
}

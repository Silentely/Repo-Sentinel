package httpapi

import (
	"net/http"
	"os"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
)

type versionResponse struct {
	Version            string              `json:"version"`
	GitSHA             string              `json:"git_sha"`
	GitBranch          string              `json:"git_branch"`
	BuildTime          string              `json:"build_time"`
	BuildChannel       string              `json:"build_channel"`
	GoVersion          string              `json:"go_version"`
	DatabaseDriver     string              `json:"database_driver"`
	SchemaVersion      string              `json:"schema_version"`
	UpdateCheckEnabled bool                `json:"update_check_enabled"`
	PublicBaseURL      string              `json:"public_base_url"`
	HTTPAddr           string              `json:"http_addr"`
	GitHub             githubStatusPayload `json:"github"`
}

// githubStatusPayload 仅暴露是否已配置，不回传 Secret 或私钥内容。
type githubStatusPayload struct {
	AppIDConfigured           bool   `json:"app_id_configured"`
	ClientIDConfigured        bool   `json:"client_id_configured"`
	PrivateKeyConfigured      bool   `json:"private_key_configured"`
	WebhookSecretConfigured   bool   `json:"webhook_secret_configured"`
	WebhookPreviousConfigured bool   `json:"webhook_previous_secret_configured"`
	ExternalPATConfigured     bool   `json:"external_pat_configured"`
	WebhookPath               string `json:"webhook_path"`
}

type versionCheckResponse struct {
	Version     versionResponse    `json:"version"`
	UpdateCheck updatecheck.Result `json:"update_check"`
}

func (s *server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.localVersion())
}

func (s *server) handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	force := strings.EqualFold(r.URL.Query().Get("force"), "true") || r.URL.Query().Get("force") == "1"
	local := s.localVersion()
	checker := s.dependencies.UpdateChecker
	if checker == nil {
		// 未装配时按配置构造临时检查器（测试与降级路径）。
		checker = &updatecheck.Checker{
			Enabled:  s.dependencies.Config.UpdateCheck.Enabled,
			CheckURL: s.dependencies.Config.UpdateCheck.URL,
			Token:    s.dependencies.Config.UpdateCheck.Token.Reveal(),
			Current:  local.Version,
		}
	}
	// 确保比较基准为当前进程版本。
	checker.Current = local.Version
	remote := checker.Check(r.Context(), force)
	writeJSON(w, http.StatusOK, versionCheckResponse{
		Version:     local,
		UpdateCheck: remote,
	})
}

func (s *server) localVersion() versionResponse {
	info := s.dependencies.BuildInfo
	enabled := s.dependencies.Config.UpdateCheck.Enabled
	if s.dependencies.UpdateChecker != nil {
		enabled = s.dependencies.UpdateChecker.Enabled
	}
	gh := s.dependencies.Config.GitHub
	privateKeyOK := false
	if path := strings.TrimSpace(gh.PrivateKeyPath); path != "" {
		if st, err := os.Stat(path); err == nil && !st.IsDir() {
			privateKeyOK = true
		}
	}
	return versionResponse{
		Version:            info.Version,
		GitSHA:             info.GitSHA,
		GitBranch:          info.GitBranch,
		BuildTime:          info.BuildTime,
		BuildChannel:       info.BuildChannel,
		GoVersion:          info.GoVersion,
		DatabaseDriver:     s.dependencies.Config.Database.Driver,
		SchemaVersion:      s.dependencies.SchemaVersion,
		UpdateCheckEnabled: enabled,
		PublicBaseURL:      s.dependencies.Config.HTTP.PublicBaseURL,
		HTTPAddr:           s.dependencies.Config.HTTP.Addr,
		GitHub: githubStatusPayload{
			AppIDConfigured:           gh.AppID > 0,
			ClientIDConfigured:        strings.TrimSpace(gh.ClientID) != "",
			PrivateKeyConfigured:      privateKeyOK,
			WebhookSecretConfigured:   strings.TrimSpace(gh.WebhookSecret.Reveal()) != "",
			WebhookPreviousConfigured: strings.TrimSpace(gh.WebhookPreviousSecret.Reveal()) != "",
			ExternalPATConfigured:     strings.TrimSpace(gh.ExternalPAT.Reveal()) != "",
			WebhookPath:               "/webhooks/github",
		},
	}
}

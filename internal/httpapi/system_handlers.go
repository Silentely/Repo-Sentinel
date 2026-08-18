package httpapi

import (
	"net/http"
	"os"
	"strconv"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
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
	AppIDSource               string `json:"app_id_source,omitempty"`
	ClientIDSource            string `json:"client_id_source,omitempty"`
	PrivateKeySource          string `json:"private_key_source,omitempty"`
	WebhookSecretSource       string `json:"webhook_secret_source,omitempty"`
	PublicBaseURLSource       string `json:"public_base_url_source,omitempty"`
}

type versionCheckResponse struct {
	Version     versionResponse    `json:"version"`
	UpdateCheck updatecheck.Result `json:"update_check"`
}

func (s *server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.localVersion())
}

func (s *server) handleVersionCheck(w http.ResponseWriter, r *http.Request) {
	force, _ := strconv.ParseBool(r.URL.Query().Get("force"))
	local := s.localVersion()
	checker := s.dependencies.UpdateChecker
	if checker == nil {
		// 未装配时按配置构造临时检查器（测试与降级路径）；补 Logger 让检查失败可留痕。
		checker = &updatecheck.Checker{
			Enabled:  s.dependencies.Config.UpdateCheck.Enabled,
			CheckURL: s.dependencies.Config.UpdateCheck.URL,
			Token:    s.dependencies.Config.UpdateCheck.Token.Reveal(),
			Current:  local.Version,
			Logger:   s.dependencies.Logger,
		}
	}
	// 比较基准在装配时即为当前进程版本，此处不得写共享 Checker（并发下构成数据竞争）。
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
	publicBase := s.dependencies.Config.HTTP.PublicBaseURL
	ghStatus := githubStatusPayload{WebhookPath: githubx.WebhookPath}
	if rt := s.dependencies.GitHubRuntime; rt != nil {
		appID, clientID, privateKey, webhook, previous, externalPAT, base, path := rt.StatusFlags()
		snap := rt.Snapshot()
		if base != "" {
			publicBase = base
		}
		ghStatus = githubStatusPayload{
			AppIDConfigured:           appID,
			ClientIDConfigured:        clientID,
			PrivateKeyConfigured:      privateKey,
			WebhookSecretConfigured:   webhook,
			WebhookPreviousConfigured: previous,
			ExternalPATConfigured:     externalPAT,
			WebhookPath:               path,
			AppIDSource:               snap.AppIDSource,
			ClientIDSource:            snap.ClientIDSource,
			PrivateKeySource:          snap.PrivateKeySource,
			WebhookSecretSource:       snap.WebhookSecretSource,
			PublicBaseURLSource:       snap.PublicBaseURLSource,
		}
	} else {
		gh := s.dependencies.Config.GitHub
		privateKeyOK := false
		if path := strings.TrimSpace(gh.PrivateKeyPath); path != "" {
			if st, err := os.Stat(path); err == nil && !st.IsDir() {
				privateKeyOK = true
			}
		}
		ghStatus = githubStatusPayload{
			AppIDConfigured:           gh.AppID > 0,
			ClientIDConfigured:        strings.TrimSpace(gh.ClientID) != "",
			PrivateKeyConfigured:      privateKeyOK,
			WebhookSecretConfigured:   strings.TrimSpace(gh.WebhookSecret.Reveal()) != "",
			WebhookPreviousConfigured: strings.TrimSpace(gh.WebhookPreviousSecret.Reveal()) != "",
			ExternalPATConfigured:     strings.TrimSpace(gh.ExternalPAT.Reveal()) != "",
			WebhookPath:               githubx.WebhookPath,
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
		PublicBaseURL:      publicBase,
		HTTPAddr:           s.dependencies.Config.HTTP.Addr,
		GitHub:             ghStatus,
	}
}

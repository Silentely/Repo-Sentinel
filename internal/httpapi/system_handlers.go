package httpapi

import (
	"net/http"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/updatecheck"
)

type versionResponse struct {
	Version            string `json:"version"`
	GitSHA             string `json:"git_sha"`
	GitBranch          string `json:"git_branch"`
	BuildTime          string `json:"build_time"`
	BuildChannel       string `json:"build_channel"`
	GoVersion          string `json:"go_version"`
	DatabaseDriver     string `json:"database_driver"`
	SchemaVersion      string `json:"schema_version"`
	UpdateCheckEnabled bool   `json:"update_check_enabled"`
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
	}
}

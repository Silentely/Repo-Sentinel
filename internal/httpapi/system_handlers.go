package httpapi

import "net/http"

type versionResponse struct {
	Version        string `json:"version"`
	GitSHA         string `json:"git_sha"`
	GitBranch      string `json:"git_branch"`
	BuildTime      string `json:"build_time"`
	BuildChannel   string `json:"build_channel"`
	GoVersion      string `json:"go_version"`
	DatabaseDriver string `json:"database_driver"`
	SchemaVersion  string `json:"schema_version"`
}

func (s *server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	info := s.dependencies.BuildInfo
	writeJSON(w, http.StatusOK, versionResponse{
		Version:        info.Version,
		GitSHA:         info.GitSHA,
		GitBranch:      info.GitBranch,
		BuildTime:      info.BuildTime,
		BuildChannel:   info.BuildChannel,
		GoVersion:      info.GoVersion,
		DatabaseDriver: s.dependencies.Config.Database.Driver,
		SchemaVersion:  s.dependencies.SchemaVersion,
	})
}

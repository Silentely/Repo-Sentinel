package httpapi

import (
	"net/http"

	"github.com/Silentely/Repo-Sentinel/internal/plugin"
)

func (s *server) handleListPlugins(w http.ResponseWriter, r *http.Request) {
	if s.pluginManager == nil {
		writeJSON(w, http.StatusOK, map[string]any{"plugins": []plugin.PluginInfo{}})
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"plugins": s.pluginManager.List(r.Context())})
}

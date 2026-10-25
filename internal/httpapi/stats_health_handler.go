package httpapi

import (
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/stats"
)

func (s *server) handleRepositoryHealth(w http.ResponseWriter, r *http.Request) {
	repoID := strings.TrimSpace(r.URL.Query().Get("repository_id"))
	if s.dependencies.Store == nil {
		s.writeAPIError(w, r, http.StatusServiceUnavailable, "store_unavailable", map[string]any{"message": "存储服务未就绪"})
		return
	}

	if repoID != "" {
		if _, err := s.dependencies.Store.Repositories().Get(r.Context(), repoID); err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}

	report, err := stats.CalculateHealthScorecard(r.Context(), s.dependencies.Store, repoID, time.Now().UTC())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	writeJSON(w, http.StatusOK, report)
}

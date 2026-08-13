package httpapi

import "net/http"

// noStore 供探活/就绪端点使用：健康检查响应不得被中间层缓存
// （否则 503 会被缓存，编排系统误判实例健康）。
func (s *server) handleLive(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (s *server) handleReady(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	if err := s.dependencies.Ready.Ready(r.Context()); err != nil {
		// 就绪失败的具体原因（DB 不可达/迁移失败等）进日志，503 只回状态码：
		// 排障时不必再猜是哪类依赖出问题。
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("ready check failed",
				"request_id", requestIDFromContext(r.Context()),
				"error_code", "not_ready",
				"error", err.Error())
		}
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"status": "not_ready"})
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"status": "ready"})
}

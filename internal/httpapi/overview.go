package httpapi

import (
	"encoding/json"
	"net/http"
	"time"
)

// SystemOverviewSnapshot 描述实时系统全景快照
type SystemOverviewSnapshot struct {
	UptimeSeconds   int64     `json:"uptime_seconds"`
	QueuePending    int       `json:"queue_pending"`
	SuccessRate24h  float64   `json:"success_rate_24h"`
	ActiveChannels  []string  `json:"active_channels"`
	QuietHoursActive bool     `json:"quiet_hours_active"`
	Timestamp       time.Time `json:"timestamp"`
}

// SystemOverviewHandler 提供 GET /system/overview 接口服务
func SystemOverviewHandler(startTime time.Time, snapshotProvider func() SystemOverviewSnapshot) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var snapshot SystemOverviewSnapshot
		if snapshotProvider != nil {
			snapshot = snapshotProvider()
		}

		snapshot.Timestamp = time.Now()
		if !startTime.IsZero() {
			snapshot.UptimeSeconds = int64(time.Since(startTime).Seconds())
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_ = json.NewEncoder(w).Encode(snapshot)
	}
}

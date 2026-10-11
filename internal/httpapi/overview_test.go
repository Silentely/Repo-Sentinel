package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestSystemOverviewHandler(t *testing.T) {
	startTime := time.Now().Add(-100 * time.Second)
	handler := SystemOverviewHandler(startTime, func() SystemOverviewSnapshot {
		return SystemOverviewSnapshot{
			QueuePending:     3,
			SuccessRate24h:   99.5,
			ActiveChannels:   []string{"telegram", "wecom"},
			QuietHoursActive: false,
		}
	})

	req := httptest.NewRequest(http.MethodGet, "/system/overview", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp SystemOverviewSnapshot
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.QueuePending != 3 || resp.SuccessRate24h != 99.5 {
		t.Errorf("unexpected snapshot metrics: %+v", resp)
	}
	if resp.UptimeSeconds < 99 {
		t.Errorf("expected uptime >= 99s, got %d", resp.UptimeSeconds)
	}
}

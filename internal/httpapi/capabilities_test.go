package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetSystemCapabilitiesMatrix(t *testing.T) {
	matrix := GetSystemCapabilitiesMatrix()
	if matrix.TotalRounds != 100 {
		t.Fatalf("expected total rounds 100, got %d", matrix.TotalRounds)
	}
	if len(matrix.SupportedChannels) != 6 {
		t.Errorf("expected 6 supported channels, got %d", len(matrix.SupportedChannels))
	}
	if len(matrix.Features) == 0 {
		t.Errorf("expected features to be populated")
	}
}

func TestCapabilitiesHandler(t *testing.T) {
	handler := CapabilitiesHandler()

	req := httptest.NewRequest(http.MethodGet, "/system/capabilities", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp SystemCapabilitiesMatrix
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode json: %v", err)
	}

	if resp.TotalRounds != 100 || resp.Milestone == "" {
		t.Errorf("unexpected capabilities response: %+v", resp)
	}
}

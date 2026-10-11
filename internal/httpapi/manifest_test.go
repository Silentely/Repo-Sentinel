package httpapi

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestBuildSystemManifest(t *testing.T) {
	channels := []string{"telegram", "feishu", "slack"}
	manifest := BuildSystemManifest("v2.4.0", "da6fad7", channels, 12)

	if manifest.Version != "v2.4.0" {
		t.Errorf("expected version v2.4.0, got %s", manifest.Version)
	}
	if manifest.GitCommit != "da6fad7" {
		t.Errorf("expected commit da6fad7, got %s", manifest.GitCommit)
	}
	if len(manifest.EnabledChannels) != 3 {
		t.Errorf("expected 3 channels, got %d", len(manifest.EnabledChannels))
	}
	if len(manifest.Subsystems) == 0 {
		t.Errorf("expected subsystems to be populated")
	}
}

func TestSystemManifestHandler(t *testing.T) {
	handler := SystemManifestHandler(func() SystemManifest {
		return BuildSystemManifest("v2.4.0", "abcdef", []string{"telegram"}, 5)
	})

	req := httptest.NewRequest(http.MethodGet, "/system/manifest", nil)
	w := httptest.NewRecorder()
	handler(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", w.Code)
	}

	var resp SystemManifest
	if err := json.Unmarshal(w.Body.Bytes(), &resp); err != nil {
		t.Fatalf("failed to decode response: %v", err)
	}

	if resp.Version != "v2.4.0" || resp.GitCommit != "abcdef" {
		t.Errorf("unexpected manifest response: %+v", resp)
	}
}

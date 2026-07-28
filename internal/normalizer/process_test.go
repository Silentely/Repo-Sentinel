package normalizer_test

import (
	"encoding/json"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestProcessIssueOpenedCreatesEvent(t *testing.T) {
	dbURL := "file:" + filepath.Join(t.TempDir(), "n.db")
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })

	payload, _ := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": 7,
			"title":  "hello",
			"state":  "open",
			"html_url": "https://github.com/acme/demo/issues/7",
			"user": map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels": []any{},
			"assignees": []any{},
		},
		"repository": map[string]any{
			"id": 99,
			"name": "demo",
			"full_name": "acme/demo",
			"private": false,
			"html_url": "https://github.com/acme/demo",
			"default_branch": "main",
			"owner": map[string]any{"login": "acme"},
		},
	})

	proc := &normalizer.Processor{Store: data}
	res, err := proc.Process(t.Context(), "issues", "delivery-1", payload)
	if err != nil {
		t.Fatal(err)
	}
	if res.Event == nil {
		t.Fatal("期望产生事件")
	}
	if res.Repository == nil || res.Repository.FullName != "acme/demo" {
		t.Fatalf("仓库不正确: %+v", res.Repository)
	}
	// 基线中应抑制通知
	if !res.SuppressNotify {
		t.Fatal("新仓库应处于基线抑制")
	}
}

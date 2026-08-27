package normalizer

import (
	"context"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func openStore(t *testing.T) store.Store {
	t.Helper()
	dbURL := "file:" + t.TempDir() + "/repo.db"
	data, err := store.Open(t.Context(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

func TestNormalizeRepositoryCreatesNew(t *testing.T) {
	data := openStore(t)
	gh := &ghRepository{
		ID: 101, Name: "demo", FullName: "acme/demo", Private: false,
		HTMLURL: "https://github.com/acme/demo", DefaultBranch: "main", Archived: false,
		Owner: struct {
			Login string `json:"login"`
		}{Login: "acme"},
	}
	repo, err := NormalizeRepository(context.Background(), data, gh, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.ID == "" {
		t.Fatal("expected repo ID")
	}
	if repo.FullName != "acme/demo" {
		t.Fatalf("full_name mismatch: %s", repo.FullName)
	}
	if repo.SyncStatus != store.SyncStatusBaseline {
		t.Fatalf("expected baseline, got %s", repo.SyncStatus)
	}
}

func TestNormalizeRepositoryArchivedLinksSettings(t *testing.T) {
	data := openStore(t)
	existing, err := data.Repositories().Upsert(context.Background(), store.Repository{
		Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo",
		IssuesEnabled: true, PrEnabled: true, ActionsEnabled: true, AlertsEnabled: true,
	})
	if err != nil {
		t.Fatal(err)
	}
	gh := &ghRepository{
		ID: 101, Name: "demo", FullName: "acme/demo", Archived: true,
		Owner: struct {
			Login string `json:"login"`
		}{Login: "acme"},
	}
	repo, err := NormalizeRepository(context.Background(), data, gh, nil, nil)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if repo.SyncStatus != store.SyncStatusArchived {
		t.Fatalf("expected archived sync status, got %s", repo.SyncStatus)
	}
	loaded, err := data.Repositories().Get(context.Background(), repo.ID)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ID != existing.ID {
		t.Fatalf("expected same repo id, got %s vs %s", loaded.ID, existing.ID)
	}
}

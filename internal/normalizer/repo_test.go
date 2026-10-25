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

func BenchmarkNormalizeRepository(b *testing.B) {
	data := openStoreBench(b)
	gh := &ghRepository{
		ID: 101, Name: "demo", FullName: "acme/demo", Private: false,
		HTMLURL: "https://github.com/acme/demo", DefaultBranch: "main", Archived: false,
		Owner: struct {
			Login string `json:"login"`
		}{Login: "acme"},
	}
	ctx := context.Background()
	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, err := NormalizeRepository(ctx, data, gh, nil, nil)
		if err != nil {
			b.Fatal(err)
		}
	}
}

func openStoreBench(b *testing.B) store.Store {
	b.Helper()
	dbURL := "file:" + b.TempDir() + "/repo_bench.db"
	data, err := store.Open(context.Background(), config.DatabaseConfig{Driver: "sqlite", URL: dbURL})
	if err != nil {
		b.Fatal(err)
	}
	b.Cleanup(func() { _ = data.Close() })
	return data
}

func TestNormalizeRepository_RenamePreservesIDAndUpdatesFullName(t *testing.T) {
	ctx := context.Background()
	data := openStore(t)

	origID := int64(12345678)
	gh1 := &ghRepository{
		ID: origID, Name: "my-repo", FullName: "old-org/my-repo", Private: false,
		HTMLURL: "https://github.com/old-org/my-repo", DefaultBranch: "main", Archived: false,
		Owner: struct {
			Login string `json:"login"`
		}{Login: "old-org"},
	}
	repo1, err := NormalizeRepository(ctx, data, gh1, nil, nil)
	if err != nil {
		t.Fatalf("initial normalize failed: %v", err)
	}

	gh2 := &ghRepository{
		ID: origID, Name: "my-repo", FullName: "new-org/my-repo", Private: false,
		HTMLURL: "https://github.com/new-org/my-repo", DefaultBranch: "main", Archived: false,
		Owner: struct {
			Login string `json:"login"`
		}{Login: "new-org"},
	}
	repo2, err := NormalizeRepository(ctx, data, gh2, nil, nil)
	if err != nil {
		t.Fatalf("renamed normalize failed: %v", err)
	}

	if repo2.ID != repo1.ID {
		t.Errorf("expected repo ID to be preserved across rename, got %s != %s", repo2.ID, repo1.ID)
	}
	if repo2.FullName != "new-org/my-repo" {
		t.Errorf("expected FullName to be updated, got %s", repo2.FullName)
	}

	byNew, err := data.Repositories().GetByFullName(ctx, "new-org/my-repo")
	if err != nil || byNew.ID != repo1.ID {
		t.Errorf("lookup by new name failed: %v", err)
	}
}

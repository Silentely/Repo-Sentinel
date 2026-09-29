package store_test

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestSQLiteMaintenance_DedicatedConnectionAndConcurrentPing(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dbPath := "file:" + filepath.Join(t.TempDir(), "maint-test.db")

	// 1. Open primary store
	mainStore, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          dbPath,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open main store: %v", err)
	}
	defer mainStore.Close()

	// 2. Open dedicated maintenance connection
	maintDB, err := store.OpenMaintenanceDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open maintenance DB: %v", err)
	}
	defer maintDB.Close()

	// 3. Verify busy_timeout pragma on maintenance DB is 1000ms
	var busyTimeout int
	if err := maintDB.QueryRowContext(t.Context(), "PRAGMA busy_timeout").Scan(&busyTimeout); err != nil {
		t.Fatalf("failed to query busy_timeout on maintDB: %v", err)
	}
	if busyTimeout != 1000 {
		t.Fatalf("expected busy_timeout=1000 on maintDB, got %d", busyTimeout)
	}

	// 4. Concurrently run maintenance task while pinging main store
	done := make(chan struct{})
	go func() {
		defer close(done)
		for i := 0; i < 5; i++ {
			if err := store.RunMaintenanceTask(t.Context(), maintDB); err != nil {
				t.Errorf("maintenance task failed: %v", err)
			}
			time.Sleep(10 * time.Millisecond)
		}
	}()

	// PingQuick should succeed quickly without being starved
	for i := 0; i < 10; i++ {
		pingCtx, cancel := context.WithTimeout(t.Context(), 500*time.Millisecond)
		err := mainStore.PingQuick(pingCtx)
		cancel()
		if err != nil {
			t.Fatalf("PingQuick failed during concurrent maintenance (round %d): %v", i, err)
		}
		time.Sleep(5 * time.Millisecond)
	}

	<-done
}

func TestSQLiteMaintenance_ContextCancellation(t *testing.T) {
	t.Setenv("TMPDIR", t.TempDir())
	dbPath := "file:" + filepath.Join(t.TempDir(), "maint-cancel.db")

	maintDB, err := store.OpenMaintenanceDB(dbPath)
	if err != nil {
		t.Fatalf("failed to open maintenance DB: %v", err)
	}
	defer maintDB.Close()

	ctx, cancel := context.WithCancel(t.Context())
	cancel() // canceled immediately

	err = store.RunMaintenanceTask(ctx, maintDB)
	if err == nil {
		t.Fatal("expected error on canceled context, got nil")
	}
}

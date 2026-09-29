package store_test

import (
	"fmt"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestWorkItem_BatchUpsert_Chunking(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file::memory:?cache=shared",
		MaxOpenConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	// 1. Seed a test repo
	repo, err := db.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-batch-1",
		FullName:       "org/batch-test",
		MonitorEnabled: true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	// 2. Prepare 500 work items (exceeding chunk size of 200)
	items := make([]store.WorkItem, 500)
	now := time.Now().UTC()
	for i := 0; i < 500; i++ {
		items[i] = store.WorkItem{
			RepositoryID:    repo.ID,
			Number:          i + 1,
			Kind:            store.WorkItemKindIssue,
			State:           "open",
			Title:           fmt.Sprintf("Issue %d", i+1),
			Author:          "dev",
			SourceUpdatedAt: now,
			StateHash:       fmt.Sprintf("hash-%d", i+1),
		}
	}

	// 3. Batch upsert
	err = db.WorkItems().BatchUpsert(ctx, items)
	if err != nil {
		t.Fatalf("BatchUpsert failed: %v", err)
	}

	// 4. Verify count
	count, err := db.WorkItems().CountOpen(ctx)
	if err != nil {
		t.Fatalf("CountOpen failed: %v", err)
	}
	if count != 500 {
		t.Fatalf("expected 500 open items, got %d", count)
	}
}

func TestWorkItem_StateMachine_AntiReplayClosed(t *testing.T) {
	ctx := t.Context()
	db, err := store.Open(ctx, config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file::memory:?cache=shared",
		MaxOpenConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open store: %v", err)
	}
	defer db.Close()

	repo, err := db.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-sm-1",
		FullName:       "org/sm-test",
		MonitorEnabled: true,
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("failed to seed repo: %v", err)
	}

	t1 := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	t2 := time.Date(2026, 9, 29, 11, 0, 0, 0, time.UTC)
	t3 := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)

	// Step 1: Issue closed at t2
	_, updated, err := db.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          10,
		Kind:            store.WorkItemKindIssue,
		State:           "closed",
		Title:           "Closed issue",
		Author:          "author",
		SourceUpdatedAt: t2,
		StateHash:       "hash-closed",
	}, nil)
	if err != nil || !updated {
		t.Fatalf("failed to insert closed item: %v (updated=%v)", err, updated)
	}

	// Step 2: Out of order webhook arrives with state=open, but SourceUpdatedAt=t1 (earlier than t2)
	// Must be rejected by anti-replay / state machine
	_, updated, err = db.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          10,
		Kind:            store.WorkItemKindIssue,
		State:           "open",
		Title:           "Reopened? No, out-of-order",
		Author:          "author",
		SourceUpdatedAt: t1,
		StateHash:       "hash-open-old",
	}, nil)
	if err != nil {
		t.Fatalf("unexpected error on older open item: %v", err)
	}
	if updated {
		t.Fatal("expected older open item to NOT update existing closed item")
	}

	item, err := db.WorkItems().GetByRepoNumber(ctx, repo.ID, 10)
	if err != nil {
		t.Fatalf("failed to get item: %v", err)
	}
	if item.State != "closed" {
		t.Fatalf("expected state to remain closed, got %s", item.State)
	}

	// Step 3: Legitimate reopen with SourceUpdatedAt=t3 (strictly newer than t2)
	// Must be accepted
	_, updated, err = db.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		RepositoryID:    repo.ID,
		Number:          10,
		Kind:            store.WorkItemKindIssue,
		State:           "open",
		Title:           "Legitimately reopened",
		Author:          "author",
		SourceUpdatedAt: t3,
		StateHash:       "hash-reopened",
	}, nil)
	if err != nil || !updated {
		t.Fatalf("expected legitimate reopen to succeed, updated=%v, err=%v", updated, err)
	}

	item, err = db.WorkItems().GetByRepoNumber(ctx, repo.ID, 10)
	if err != nil {
		t.Fatalf("failed to get item: %v", err)
	}
	if item.State != "open" {
		t.Fatalf("expected state to be reopened (open), got %s", item.State)
	}
}

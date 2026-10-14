package chatops

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func newTestStore(t *testing.T) store.Store {
	t.Helper()
	tempDir := t.TempDir()
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(tempDir, "chatops_test.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("failed to open test store: %v", err)
	}
	t.Cleanup(func() {
		_ = opened.Close()
	})
	return opened
}

func TestTokenStore_AntiReplay(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action:  "workflow_rerun",
		RepoID:  "repo-1",
		RunID:   "123456",
		ActorID: "user-1",
	}, 10*time.Minute)
	if err != nil {
		t.Fatalf("failed to create token: %v", err)
	}

	data, err := ts.Consume(ctx, tokenID)
	if err != nil {
		t.Fatalf("first consume should succeed, got: %v", err)
	}
	if data.Action != "workflow_rerun" || data.RepoID != "repo-1" {
		t.Fatalf("unexpected data: %+v", data)
	}

	// 第二次消费必须失败（防重放）
	_, err = ts.Consume(ctx, tokenID)
	if err == nil {
		t.Fatal("expected second consume to fail, got nil")
	}
}

func TestTokenStore_ConcurrentConsumptionAllowsOnlyOneWinner(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action: "workflow_rerun",
		RepoID: "repo-1",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	const concurrency = 8
	results := make(chan error, concurrency)
	for i := 0; i < concurrency; i++ {
		go func() {
			_, err := ts.Consume(ctx, tokenID)
			results <- err
		}()
	}

	winners := 0
	for i := 0; i < concurrency; i++ {
		if <-results == nil {
			winners++
		}
	}
	if winners != 1 {
		t.Fatalf("expected exactly one winner, got %d", winners)
	}
}

func TestTokenStore_ReleaseAllowsRetry(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	tokenID, err := ts.Create(ctx, ActionData{
		Action: "workflow_rerun",
		RepoID: "repo-1",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ts.Consume(ctx, tokenID); err != nil {
		t.Fatal(err)
	}

	// 释放后允许再次消费
	if err := ts.Release(ctx, tokenID); err != nil {
		t.Fatal(err)
	}

	if _, err := ts.Consume(ctx, tokenID); err != nil {
		t.Fatalf("released token should be retryable: %v", err)
	}
}

func TestTokenStore_ReleaseSurvivesCanceledContext(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)

	tokenID, err := ts.Create(context.Background(), ActionData{
		Action: "workflow_rerun",
		RepoID: "repo-1",
	}, 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := ts.Consume(context.Background(), tokenID); err != nil {
		t.Fatal(err)
	}

	// 模拟已取消的请求上下文
	canceledCtx, cancel := context.WithCancel(context.Background())
	cancel()

	// Release 内部应使用 context.WithoutCancel 确保回滚落库
	if err := ts.Release(canceledCtx, tokenID); err != nil {
		t.Fatalf("release should survive canceled parent context: %v", err)
	}

	// 回滚后可再次正常消费
	if _, err := ts.Consume(context.Background(), tokenID); err != nil {
		t.Fatalf("token should be retryable after release: %v", err)
	}
}

func TestTokenStore_Expiry(t *testing.T) {
	st := newTestStore(t)
	ts := NewTokenStore(st)
	ctx := t.Context()

	// 创建已过期的 Token
	tokenID, err := ts.Create(ctx, ActionData{
		Action: "workflow_rerun",
		RepoID: "repo-1",
	}, -1*time.Minute)
	if err != nil {
		t.Fatal(err)
	}

	_, err = ts.Consume(ctx, tokenID)
	if err == nil {
		t.Fatal("expected expired token to fail consumption")
	}
}

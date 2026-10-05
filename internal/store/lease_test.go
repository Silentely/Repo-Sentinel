package store_test

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func openTestStoreForLease(t *testing.T) store.Store {
	t.Helper()
	cfg := config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + t.TempDir() + "/lease-test.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
	s, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openTestStoreForLease failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestLease_ConcurrentAcquisition(t *testing.T) {
	st := openTestStoreForLease(t)
	ctx := t.Context()
	taskName := "sync_all_repos"

	const concurrentWorkers = 10
	var wg sync.WaitGroup
	var successCount int64
	var failedCount int64
	var winningToken int64

	for i := 0; i < concurrentWorkers; i++ {
		wg.Add(1)
		go func(workerIndex int) {
			defer wg.Done()
			holderID := "pod-" + string(rune('A'+workerIndex))
			token, ok, err := st.Leases().Acquire(ctx, taskName, holderID, 5*time.Second)
			if err != nil {
				t.Errorf("worker %s acquire error: %v", holderID, err)
				return
			}
			if ok {
				atomic.AddInt64(&successCount, 1)
				atomic.StoreInt64(&winningToken, token)
			} else {
				atomic.AddInt64(&failedCount, 1)
			}
		}(i)
	}

	wg.Wait()

	if successCount != 1 {
		t.Fatalf("期望仅有 1 个 worker 成功抢占租约，实际成功数: %d", successCount)
	}
	if failedCount != concurrentWorkers-1 {
		t.Fatalf("期望其余 %d 个 worker 抢占失败，实际失败数: %d", concurrentWorkers-1, failedCount)
	}
	if winningToken <= 0 {
		t.Fatalf("期望获胜者的 fencingToken > 0，实际: %d", winningToken)
	}
}

func TestLease_ReentrancyKeepsToken(t *testing.T) {
	st := openTestStoreForLease(t)
	ctx := t.Context()
	taskName := "poll_starred_releases"
	holderID := "worker-alpha"

	token1, ok, err := st.Leases().Acquire(ctx, taskName, holderID, 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("第 1 次 Acquire 失败: ok=%v, err=%v", ok, err)
	}

	// 租约有效期内同一 holder 重入
	token2, ok, err := st.Leases().Acquire(ctx, taskName, holderID, 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("重入 Acquire 失败: ok=%v, err=%v", ok, err)
	}

	if token1 != token2 {
		t.Fatalf("同一 holder 有效期内重入 fencingToken 必须保持不自增: token1=%d, token2=%d", token1, token2)
	}
}

func TestLease_RenewAndReleaseLifecycle(t *testing.T) {
	st := openTestStoreForLease(t)
	ctx := t.Context()
	taskName := "daily_report"
	holderID := "worker-beta"

	token, ok, err := st.Leases().Acquire(ctx, taskName, holderID, 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("Acquire 失败: ok=%v, err=%v", ok, err)
	}

	// 正常续租
	renewed, err := st.Leases().Renew(ctx, taskName, holderID, token, 15*time.Second)
	if err != nil || !renewed {
		t.Fatalf("正常 Renew 期望 true, 实际: renewed=%v, err=%v", renewed, err)
	}

	// 错误 Token 续租失败
	renewedFake, err := st.Leases().Renew(ctx, taskName, holderID, token+999, 15*time.Second)
	if err != nil || renewedFake {
		t.Fatalf("错误 Token Renew 期望 false, 实际: renewed=%v, err=%v", renewedFake, err)
	}

	// 错误 Token 释放失败
	releasedFake, err := st.Leases().Release(ctx, taskName, holderID, token+999)
	if err != nil || releasedFake {
		t.Fatalf("错误 Token Release 期望 false, 实际: released=%v, err=%v", releasedFake, err)
	}

	// 正确释放
	released, err := st.Leases().Release(ctx, taskName, holderID, token)
	if err != nil || !released {
		t.Fatalf("正确 Release 期望 true, 实际: released=%v, err=%v", released, err)
	}

	// 释放后，第三方 holder 可以立即抢占，且 fencingToken 递增
	otherHolder := "worker-gamma"
	tokenNext, ok, err := st.Leases().Acquire(ctx, taskName, otherHolder, 10*time.Second)
	if err != nil || !ok {
		t.Fatalf("释放后第三方 Acquire 失败: ok=%v, err=%v", ok, err)
	}
	if tokenNext <= token {
		t.Fatalf("释放后被新 holder 抢占，fencingToken 必须单调递增: old=%d, new=%d", token, tokenNext)
	}
}

package syncx_test

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/syncx"
)

func openTestStoreForHA(t *testing.T) store.Store {
	t.Helper()
	cfg := config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + t.TempDir() + "/scheduler-ha-test.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
	s, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openTestStoreForHA failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestSchedulerHA_ConcurrentLeaseCompetition 模拟两个 Pod 并发触发周期对账任务，
// 验证只有一个 Pod 执行业务，另一个返回 Skipped=true, err=nil 安全跳过。
func TestSchedulerHA_ConcurrentLeaseCompetition(t *testing.T) {
	st := openTestStoreForHA(t)
	ctx := t.Context()

	runnerA := &syncx.LeaseRunner{
		Leases:   st.Leases(),
		WorkerID: "pod-1",
	}
	runnerB := &syncx.LeaseRunner{
		Leases:   st.Leases(),
		WorkerID: "pod-2",
	}

	taskName := "reconcile"
	var executedCount int64
	var skippedCount int64

	startCh := make(chan struct{})
	var wg sync.WaitGroup

	runWorker := func(runner *syncx.LeaseRunner) {
		defer wg.Done()
		<-startCh
		res, err := runner.RunWithLease(ctx, taskName, 5*time.Second, func(taskCtx context.Context) error {
			atomic.AddInt64(&executedCount, 1)
			time.Sleep(100 * time.Millisecond) // 模拟业务占用中
			return nil
		})
		if err != nil {
			t.Errorf("worker error: %v", err)
			return
		}
		if res.Skipped {
			atomic.AddInt64(&skippedCount, 1)
		}
	}

	wg.Add(2)
	go runWorker(runnerA)
	go runWorker(runnerB)

	close(startCh)
	wg.Wait()

	if executedCount != 1 {
		t.Fatalf("期望仅有 1 个 Pod 执行业务，实际执行次数: %d", executedCount)
	}
	if skippedCount != 1 {
		t.Fatalf("期望另 1 个 Pod 安全跳过，实际跳过次数: %d", skippedCount)
	}
}

// TestSchedulerHA_RenewalFailureCancelsContext 模拟执行过程中心跳续约失败，
// 验证业务 goroutine 的 taskCtx.Done() 立即触发取消，且后续批次检查 taskCtx.Err() 中止。
func TestSchedulerHA_RenewalFailureCancelsContext(t *testing.T) {
	st := openTestStoreForHA(t)
	ctx := t.Context()

	runner := &syncx.LeaseRunner{
		Leases:   st.Leases(),
		WorkerID: "pod-worker-timeout",
	}

	taskName := "long_running_sync"
	leaseTTL := 300 * time.Millisecond // 300ms TTL，续约间隔 100ms

	canceledCh := make(chan struct{})

	res, err := runner.RunWithLease(ctx, taskName, leaseTTL, func(taskCtx context.Context) error {
		// 模拟第三方外部更新租约 holder，导致当前 Worker 续约失败
		go func() {
			time.Sleep(50 * time.Millisecond)
			// 手动将租约强行抢占或释放为其它 holder
			_, _ = st.Leases().Release(context.Background(), taskName, "pod-worker-timeout", 1)
			_, _, _ = st.Leases().Acquire(context.Background(), taskName, "rogue-worker", 5*time.Second)
		}()

		select {
		case <-taskCtx.Done():
			close(canceledCh)
			return taskCtx.Err()
		case <-time.After(2 * time.Second):
			t.Error("taskCtx 在心跳失败后超时未收到取消信号")
			return errors.New("timeout waiting for cancel")
		}
	})

	if err == nil {
		t.Fatalf("心跳续约失败后任务期望返回非 nil 错误，实际: err=nil, res=%+v", res)
	}

	select {
	case <-canceledCh:
		// 成功收到取消
	default:
		t.Fatal("未收到 taskCtx.Done() 信号")
	}
}

// mockFailingLeaseStore 模拟数据库故障
type mockFailingLeaseStore struct {
	store.LeaseStore
}

func (m *mockFailingLeaseStore) Acquire(ctx context.Context, taskName, holderID string, ttl time.Duration) (int64, bool, error) {
	return 0, false, errors.New("connection reset by peer")
}

// TestSchedulerHA_DatabaseFailureRefusesFallback 模拟 DB 断开连接，
// 验证在多副本模式下返回 err != nil 触发系统告警，绝对严禁调用本地内存锁回退。
func TestSchedulerHA_DatabaseFailureRefusesFallback(t *testing.T) {
	runner := &syncx.LeaseRunner{
		Leases:     &mockFailingLeaseStore{},
		WorkerID:   "pod-ha-db-fail",
		SingleNode: false, // 严格多副本模式
	}

	executed := false
	res, err := runner.RunWithLease(context.Background(), "any_task", 5*time.Second, func(taskCtx context.Context) error {
		executed = true
		return nil
	})

	if err == nil {
		t.Fatalf("多副本模式下 DB 故障必须返回 error，实际: err=nil, res=%+v", res)
	}
	if executed {
		t.Fatal("严格 HA 模式下数据库故障绝对严禁回退执行业务！")
	}
}

// TestSchedulerHA_SingleNodeModeAllowsMemoryLock 验证纯本地单机模式仅在 SingleNode=true 且 Leases=nil 时方可使用内存锁。
func TestSchedulerHA_SingleNodeModeAllowsMemoryLock(t *testing.T) {
	runner := &syncx.LeaseRunner{
		Leases:     nil,
		WorkerID:   "single-node-worker",
		SingleNode: true,
	}

	executed := false
	res, err := runner.RunWithLease(context.Background(), "single_task", 5*time.Second, func(taskCtx context.Context) error {
		executed = true
		return nil
	})

	if err != nil {
		t.Fatalf("单机模式下无 LeaseStore 应降级为内存锁成功，实际返回错误: %v", err)
	}
	if !res.Acquired || !executed {
		t.Fatalf("期望执行业务成功，实际: res=%+v, executed=%v", res, executed)
	}
}

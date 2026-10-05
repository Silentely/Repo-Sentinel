package syncx

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// LeaseAcquireResult 表示租约抢占与执行结果。
type LeaseAcquireResult struct {
	Acquired bool
	Skipped  bool
}

// LeaseRunner 驱动带心跳续约与熔断中断的分布式租约执行器。
type LeaseRunner struct {
	Leases     store.LeaseStore
	WorkerID   string
	SingleNode bool
	Logger     *slog.Logger

	localMu sync.Mutex
}

// NewLeaseRunner 创建分布式租约执行器实例。
func NewLeaseRunner(leases store.LeaseStore, workerID string, logger *slog.Logger) *LeaseRunner {
	if workerID == "" {
		host, _ := os.Hostname()
		workerID = fmt.Sprintf("%s-%d", host, time.Now().UnixNano())
	}
	singleNode := os.Getenv("REPOSENTINEL_SINGLE_NODE") == "true"
	return &LeaseRunner{
		Leases:     leases,
		WorkerID:   workerID,
		SingleNode: singleNode,
		Logger:     logger,
	}
}

// RunWithLease 在持有分布式租约的情况下执行 fn。
// 1. 若非单机模式且无租约存储或数据库不可用，严格报错，杜绝静默降级为进程内锁；
// 2. 成功抢占后，启动后台 goroutine 按 TTL/3 周期续约；
// 3. 若续约失败或租约丢失，立即 cancel 业务上下文 taskCtx，中断下游写入；
// 4. fn 退出后，安全释放租约。
func (r *LeaseRunner) RunWithLease(ctx context.Context, taskName string, ttl time.Duration, fn func(taskCtx context.Context) error) (LeaseAcquireResult, error) {
	if r.Leases == nil {
		if r.SingleNode {
			// 单机显式配置允许使用本地互斥锁
			if !r.localMu.TryLock() {
				return LeaseAcquireResult{Acquired: false, Skipped: true}, nil
			}
			defer r.localMu.Unlock()
			err := fn(ctx)
			return LeaseAcquireResult{Acquired: true, Skipped: false}, err
		}
		return LeaseAcquireResult{}, errors.New("lease store is required in multi-node mode")
	}

	workerID := r.WorkerID
	if workerID == "" {
		host, _ := os.Hostname()
		workerID = fmt.Sprintf("%s-%d", host, time.Now().UnixNano())
	}

	if ttl <= 0 {
		ttl = 30 * time.Second
	}

	fencingToken, ok, err := r.Leases.Acquire(ctx, taskName, workerID, ttl)
	if err != nil {
		if r.Logger != nil {
			r.Logger.Error("failed to acquire distributed lease", "task", taskName, "worker", workerID, "error", err)
		}
		return LeaseAcquireResult{}, fmt.Errorf("acquire lease %s: %w", taskName, err)
	}
	if !ok {
		// 其它副本正常抢占持有中，安全跳过
		if r.Logger != nil {
			r.Logger.Debug("distributed lease skipped", "task", taskName, "reason", "held_by_other")
		}
		return LeaseAcquireResult{Acquired: false, Skipped: true}, nil
	}

	// 成功获取租约，包装带中断的 taskCtx
	taskCtx, cancelTask := context.WithCancel(ctx)
	defer cancelTask()

	taskDone := make(chan struct{})
	heartbeatInterval := ttl / 3
	if heartbeatInterval < 50*time.Millisecond {
		heartbeatInterval = 50 * time.Millisecond
	}

	// 启动后台心跳续约 goroutine
	go func() {
		ticker := time.NewTicker(heartbeatInterval)
		defer ticker.Stop()

		for {
			select {
			case <-taskDone:
				return
			case <-ticker.C:
				renewed, err := r.Leases.Renew(ctx, taskName, workerID, fencingToken, ttl)
				if err != nil || !renewed {
					if r.Logger != nil {
						r.Logger.Warn("distributed lease heartbeat renewal failed; interrupting task context",
							"task", taskName,
							"worker", workerID,
							"fencing_token", fencingToken,
							"error", err,
						)
					}
					cancelTask()
					return
				}
			}
		}
	}()

	// 同步执行业务函数
	fnErr := fn(taskCtx)
	close(taskDone)

	// 业务执行结束，显式释放租约
	_, _ = r.Leases.Release(ctx, taskName, workerID, fencingToken)

	return LeaseAcquireResult{Acquired: true, Skipped: false}, fnErr
}

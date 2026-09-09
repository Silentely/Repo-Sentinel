package syncx

import (
	"context"
	"errors"
	"log/slog"
	"math/rand/v2"
	"sync"
	"sync/atomic"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/digest"
)

// jitteredDuration 返回 base 的 ±10% 随机偏移（下限 1ms，避免测试毫秒级周期 jitter 为负），
// 用于错开多实例同时重启的启动风暴与持续锁步（对账/外部轮询低频任务）。
func jitteredDuration(base time.Duration) time.Duration {
	if base <= 0 {
		return base
	}
	ten := base / 10
	if ten < time.Millisecond {
		ten = time.Millisecond
	}
	return base + time.Duration(rand.Int64N(2*int64(ten)+1)-int64(ten))
}

// 启动窗口：固定延迟 45s 叠加 0~30s 随机抖动，避免与启动风暴重叠。
const (
	startupDelayBase = 45 * time.Second
	startupJitterMax = 30 * time.Second
)

// Scheduler 驱动对账、外部轮询与每日摘要。
type Scheduler struct {
	Reconciler *Reconciler
	External   *ExternalPoller
	Starred    *StarredReleasePoller
	Digest     *digest.Generator
	Logger     *slog.Logger

	ReconcileEvery time.Duration
	ExternalEvery  time.Duration
	DigestEvery    time.Duration

	reconcileBusy atomic.Bool
	externalBusy  atomic.Bool
	starredBusy   atomic.Bool
	digestBusy    atomic.Bool
}

// scheduledTaskTimeout 单次调度任务超时上限：任务挂死（外部依赖慢）时释放 select
// 循环，避免单任务阻塞全部调度（digest/star/对账互相拖累）。
const scheduledTaskTimeout = 30 * time.Minute

// runScheduledTask 统一记录调度任务失败上下文；任务在带超时的上下文内同步执行。
// 失败记 Error（error_code 稳定可聚合），成功留痕放 Debug（task + duration_ms）：
// 正常周期不刷屏，排查「任务到底跑没跑」时把 logging.level 调成 debug 即可确认。
func (s *Scheduler) runScheduledTask(ctx context.Context, task, message, errorCode string, run func(context.Context) error) {
	taskCtx, cancel := context.WithTimeout(ctx, scheduledTaskTimeout)
	defer cancel()
	startedAt := time.Now()
	err := run(taskCtx)
	durationMs := time.Since(startedAt).Milliseconds()
	if err != nil {
		if s.Logger != nil {
			s.Logger.Error(
				message,
				"task", task,
				"duration_ms", durationMs,
				"error_code", errorCode,
				"error", err.Error(),
			)
		}
		return
	}
	if s.Logger != nil {
		s.Logger.Debug("scheduled task ok", "task", task, "duration_ms", durationMs)
	}
}

// Run 阻塞运行直到 ctx 取消。
// 各调度任务（对账、外部轮询、Star/Release 轮询、统计摘要）运行在独立协程中，
// 彻底解耦高耗时全量对账与高频轮询任务，互不阻塞，并各自具备原子防重入互斥。
func (s *Scheduler) Run(ctx context.Context) {
	if s.ReconcileEvery <= 0 {
		s.ReconcileEvery = 6 * time.Hour
	}
	if s.ExternalEvery <= 0 {
		s.ExternalEvery = 10 * time.Minute
	}
	if s.DigestEvery <= 0 {
		s.DigestEvery = 1 * time.Hour
	}

	runReconcile := func() {
		if s.Reconciler == nil {
			return
		}
		if !s.reconcileBusy.CompareAndSwap(false, true) {
			if s.Logger != nil {
				s.Logger.Debug("reconcile skipped", "reason", "already_running")
			}
			return
		}
		defer s.reconcileBusy.Store(false)
		s.runScheduledTask(ctx, "reconcile", "scheduled reconcile failed", "reconcile_failed", func(taskCtx context.Context) error {
			err := s.Reconciler.ReconcileAll(taskCtx, 15)
			// 与 HTTP 手动对账并发时跳过本轮：Reconciler 内部已互斥，正常情况不触发。
			if errors.Is(err, ErrReconcileInProgress) && s.Logger != nil {
				s.Logger.Debug("reconcile skipped", "reason", "reconcile_in_progress")
				return nil
			}
			return err
		})
	}
	runExternal := func() {
		if s.External == nil {
			return
		}
		if !s.externalBusy.CompareAndSwap(false, true) {
			if s.Logger != nil {
				s.Logger.Debug("external poll skipped", "reason", "already_running")
			}
			return
		}
		defer s.externalBusy.Store(false)
		s.runScheduledTask(ctx, "external_poll", "scheduled external poll failed", "external_poll_failed", func(taskCtx context.Context) error {
			return s.External.PollAll(taskCtx)
		})
	}
	runStarredWrapped := func() {
		if s.Starred == nil {
			return
		}
		if !s.starredBusy.CompareAndSwap(false, true) {
			if s.Logger != nil {
				s.Logger.Debug("starred poll skipped", "reason", "already_running")
			}
			return
		}
		defer s.starredBusy.Store(false)
		s.runStarred(ctx)
	}
	runDigest := func() {
		if s.Digest == nil {
			return
		}
		if !s.digestBusy.CompareAndSwap(false, true) {
			if s.Logger != nil {
				s.Logger.Debug("digest skipped", "reason", "already_running")
			}
			return
		}
		defer s.digestBusy.Store(false)
		now := time.Now()
		s.runScheduledTask(ctx, "digest", "scheduled digest failed", "digest_failed", func(taskCtx context.Context) error {
			return s.Digest.RunOnce(taskCtx, now)
		})
		s.runScheduledTask(ctx, "weekly_report", "scheduled weekly report failed", "weekly_report_failed", func(taskCtx context.Context) error {
			return s.Digest.RunWeekly(taskCtx, now)
		})
		s.runScheduledTask(ctx, "monthly_report", "scheduled monthly report failed", "monthly_report_failed", func(taskCtx context.Context) error {
			return s.Digest.RunMonthly(taskCtx, now)
		})
	}

	startupDelay := startupDelayBase + time.Duration(rand.Int64N(int64(startupJitterMax)))
	var wg sync.WaitGroup

	// 1. 对账独立协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		startup := time.NewTimer(startupDelay)
		ticker := time.NewTicker(jitteredDuration(s.ReconcileEvery))
		defer startup.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-startup.C:
				runReconcile()
			case <-ticker.C:
				runReconcile()
			}
		}
	}()

	// 2. 外部仓轮询独立协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		startup := time.NewTimer(startupDelay)
		ticker := time.NewTicker(jitteredDuration(s.ExternalEvery))
		defer startup.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-startup.C:
				runExternal()
			case <-ticker.C:
				runExternal()
			}
		}
	}()

	// 3. Star 与 Release 轮询独立协程（1m 基础节拍）
	wg.Add(1)
	go func() {
		defer wg.Done()
		startup := time.NewTimer(startupDelay)
		ticker := time.NewTicker(time.Minute)
		defer startup.Stop()
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-startup.C:
				runStarredWrapped()
			case <-ticker.C:
				runStarredWrapped()
			}
		}
	}()

	// 4. 周期摘要独立协程
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(s.DigestEvery)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				runDigest()
			}
		}
	}()

	wg.Wait()
}

// runStarred 驱动 star 列表同步与 release 轮询。
// 双周期（star 同步低频 / release 轮询高频）由 Poller 按 system_settings 自判到期，
// 本方法以 1m 基础节拍被调用，天然支持设置热更新。
func (s *Scheduler) runStarred(ctx context.Context) {
	if s.Starred == nil {
		return
	}
	s.runScheduledTask(ctx, "star_sync", "scheduled star sync failed", "star_sync_failed", func(taskCtx context.Context) error {
		return s.Starred.SyncStars(taskCtx)
	})
	s.runScheduledTask(ctx, "release_poll", "scheduled release poll failed", "release_poll_failed", func(taskCtx context.Context) error {
		return s.Starred.PollReleases(taskCtx)
	})
}

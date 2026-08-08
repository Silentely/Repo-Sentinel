package syncx

import (
	"context"
	"log/slog"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/digest"
)

// Scheduler 驱动对账、外部轮询与每日摘要。
type Scheduler struct {
	Reconciler *Reconciler
	External   *ExternalPoller
	Digest     *digest.Generator
	Logger     *slog.Logger

	ReconcileEvery time.Duration
	ExternalEvery  time.Duration
	DigestEvery    time.Duration
}

// runScheduledTask 统一记录调度任务失败上下文；任务本身仍按调用方提供的顺序同步执行。
// 失败记 Error（error_code 稳定可聚合），成功留痕放 Debug（task + duration_ms）：
// 正常周期不刷屏，排查「任务到底跑没跑」时把 logging.level 调成 debug 即可确认。
func (s *Scheduler) runScheduledTask(task, message, errorCode string, run func() error) {
	startedAt := time.Now()
	err := run()
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
	// 启动后短暂延迟再跑，避免与启动风暴重叠
	startup := time.NewTimer(45 * time.Second)
	reconcileT := time.NewTicker(s.ReconcileEvery)
	externalT := time.NewTicker(s.ExternalEvery)
	digestT := time.NewTicker(s.DigestEvery)
	defer startup.Stop()
	defer reconcileT.Stop()
	defer externalT.Stop()
	defer digestT.Stop()

	runReconcile := func() {
		if s.Reconciler == nil {
			return
		}
		s.runScheduledTask("reconcile", "scheduled reconcile failed", "reconcile_failed", func() error {
			return s.Reconciler.ReconcileAll(ctx, 15)
		})
	}
	runExternal := func() {
		if s.External == nil {
			return
		}
		s.runScheduledTask("external_poll", "scheduled external poll failed", "external_poll_failed", func() error {
			return s.External.PollAll(ctx)
		})
	}
	runDigest := func() {
		if s.Digest == nil {
			return
		}
		now := time.Now()
		s.runScheduledTask("digest", "scheduled digest failed", "digest_failed", func() error {
			return s.Digest.RunOnce(ctx, now)
		})
		s.runScheduledTask("weekly_report", "scheduled weekly report failed", "weekly_report_failed", func() error {
			return s.Digest.RunWeekly(ctx, now)
		})
		s.runScheduledTask("monthly_report", "scheduled monthly report failed", "monthly_report_failed", func() error {
			return s.Digest.RunMonthly(ctx, now)
		})
	}

	for {
		select {
		case <-ctx.Done():
			return
		case <-startup.C:
			runReconcile()
			runExternal()
		case <-reconcileT.C:
			runReconcile()
		case <-externalT.C:
			runExternal()
		case <-digestT.C:
			runDigest()
		}
	}
}

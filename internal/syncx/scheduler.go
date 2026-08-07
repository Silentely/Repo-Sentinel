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
		if err := s.Reconciler.ReconcileAll(ctx, 15); err != nil && s.Logger != nil {
			s.Logger.Error("scheduled reconcile failed", "error_code", "reconcile_failed", "error", err.Error())
		}
	}
	runExternal := func() {
		if s.External == nil {
			return
		}
		if err := s.External.PollAll(ctx); err != nil && s.Logger != nil {
			s.Logger.Error("scheduled external poll failed", "error_code", "external_poll_failed", "error", err.Error())
		}
	}
	runDigest := func() {
		if s.Digest == nil {
			return
		}
		now := time.Now()
		if err := s.Digest.RunOnce(ctx, now); err != nil && s.Logger != nil {
			s.Logger.Error("scheduled digest failed", "error_code", "digest_failed", "error", err.Error())
		}
		if err := s.Digest.RunWeekly(ctx, now); err != nil && s.Logger != nil {
			s.Logger.Error("scheduled weekly report failed", "error_code", "weekly_report_failed", "error", err.Error())
		}
		if err := s.Digest.RunMonthly(ctx, now); err != nil && s.Logger != nil {
			s.Logger.Error("scheduled monthly report failed", "error_code", "monthly_report_failed", "error", err.Error())
		}
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

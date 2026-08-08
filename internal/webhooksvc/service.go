// Package webhooksvc 承载 GitHub Webhook 的业务管线：
// 验签后的载荷 → 规范化 → 实时通知决策 → WebhookDelivery 状态机。
// 从 httpapi 抽出，使 HTTP 层只负责请求/响应适配，不再编排领域流程。
package webhooksvc

import (
	"context"
	"log/slog"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Evaluator 实时通知评估器（聚合器或引擎均可实现）。
type Evaluator interface {
	Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error
}

// Service 处理验签后的 Webhook 载荷并更新投递状态。
type Service struct {
	Store  store.Store
	Logger *slog.Logger
	// Evaluator 实时通知决策器；nil 时回退内置 rules.Engine。
	Evaluator Evaluator
	// AI 可选；默认 rules.Engine 的安全告警分诊客户端。
	AI *ai.Client
	// Background 后台任务生命周期；关闭时由 App 取消。
	Background context.Context
}

// Process 执行规范化 → 通知 → 状态机标记的完整管线。
// 与 HTTP 请求解耦：行状态标记使用不受取消影响的 context，保证关闭期间不残留 accepted 行。
func (s *Service) Process(rowID, eventType, deliveryID string, body []byte) {
	ctx := s.Background
	if ctx == nil {
		return
	}
	startedAt := time.Now()
	// 关闭期间 Background 已取消：状态标记必须脱离取消，否则行永久停留在 accepted。
	markCtx, markCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer markCancel()
	proc := &normalizer.Processor{Store: s.Store}
	res, err := proc.Process(ctx, eventType, deliveryID, body)
	if err != nil {
		_ = s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryFailed, "normalize_failed")
		// 规范化失败时仓库信息尚未解析出来，repo 留空由调用方从日志链路定位。
		s.logError("webhook normalize failed", deliveryID, eventType, "normalize_failed", "", err.Error(), time.Since(startedAt).Milliseconds())
		return
	}
	repoName := ""
	if res.Repository != nil {
		repoName = res.Repository.FullName
	}
	if res.Event != nil && !res.SuppressNotify {
		var err error
		if s.Evaluator != nil {
			err = s.Evaluator.Evaluate(ctx, res, repoName)
		} else {
			err = (&rules.Engine{Store: s.Store, AI: s.AI, Logger: s.Logger}).Evaluate(ctx, res, repoName)
		}
		if err != nil {
			// 通知已丢：状态必须可查，标记为失败而不是 processed。
			s.logError("rule evaluate failed", deliveryID, eventType, "rule_failed", repoName, err.Error(), time.Since(startedAt).Milliseconds())
			_ = s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryFailed, "rule_failed")
			return
		}
	}
	_ = s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryProcessed, "")
	if s.Logger != nil {
		attrs := []any{
			"delivery_id", deliveryID,
			"event_type", eventType,
			"repo", repoName,
			"updated", res.Updated,
			"suppressed", res.SuppressNotify,
			"duration_ms", time.Since(startedAt).Milliseconds(),
		}
		// 有事件时带出事件类型，便于按 kind 聚合处理量与耗时。
		if res.Event != nil {
			attrs = append(attrs, "event_kind", res.Event.Kind)
		}
		s.Logger.Info("github webhook processed", attrs...)
	}
}

func (s *Service) logError(msg, deliveryID, eventType, code, repoName, errMsg string, durationMs int64) {
	if s.Logger == nil {
		return
	}
	attrs := []any{
		"delivery_id", deliveryID,
		"event_type", eventType,
		"error_code", code,
		"error", errMsg,
		"duration_ms", durationMs,
	}
	// repo 为空（如规范化失败）时不带该字段，保持日志字段稳定。
	if repoName != "" {
		attrs = append(attrs, "repo", repoName)
	}
	s.Logger.Error(msg, attrs...)
}

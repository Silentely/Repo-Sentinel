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
	// OnFailed 可选指标回调：规范化或规则评估失败时触发（与 notify 的 OnSent 同模式，
	// 避免 webhooksvc 反向依赖 httpapi）。
	OnFailed func()
	// SlowThreshold 慢处理判定阈值；<=0 时用默认 slowWebhookThreshold。
	SlowThreshold time.Duration
}

// slowWebhookThreshold 单条 webhook 处理的慢阈值：超过说明规范化/评估路径存在
// 阻塞（数据库抖动、外部调用），以 Warn 留痕便于定位。
const slowWebhookThreshold = 5 * time.Second

// markFailed 统一处理失败分支：标记投递失败（带语义化错误码）、记录失败指标回调。
func (s *Service) markFailed(markCtx context.Context, rowID, errorCode string) {
	_ = s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryFailed, errorCode)
	if s.OnFailed != nil {
		s.OnFailed()
	}
}

// slowThreshold 返回慢处理阈值；实例未设置时用默认值。
func (s *Service) slowThreshold() time.Duration {
	if s.SlowThreshold > 0 {
		return s.SlowThreshold
	}
	return slowWebhookThreshold
}

// Process 执行规范化 → 通知 → 状态机标记的完整管线。
// 与 HTTP 请求解耦：行状态标记使用不受取消影响的 context，保证关闭期间不残留 accepted 行。
func (s *Service) Process(rowID, eventType, deliveryID string, body []byte) {
	ctx := s.Background
	if ctx == nil {
		return
	}
	startedAt := time.Now()
	// 慢处理留痕：repoName 为变量，defer 读取 return 时的最终值。
	repoName := ""
	defer func() {
		if elapsed := time.Since(startedAt); elapsed >= s.slowThreshold() && s.Logger != nil {
			s.Logger.Warn(
				"webhook process slow",
				"delivery_id", deliveryID,
				"event_type", eventType,
				"repo", repoName,
				"duration_ms", elapsed.Milliseconds(),
				"error_code", "webhook_slow",
			)
		}
	}()
	// 关闭期间 Background 已取消：状态标记必须脱离取消，否则行永久停留在 accepted。
	markCtx, markCancel := context.WithTimeout(context.WithoutCancel(ctx), 5*time.Second)
	defer markCancel()
	proc := &normalizer.Processor{Store: s.Store, Logger: s.Logger}
	res, err := proc.Process(ctx, eventType, deliveryID, body)
	if err != nil {
		s.markFailed(markCtx, rowID, "normalize_failed")
		// 规范化失败时仓库信息尚未解析出来，repo 留空由调用方从日志链路定位。
		s.logError("webhook normalize failed", deliveryID, eventType, "normalize_failed", "", err.Error(), time.Since(startedAt).Milliseconds())
		return
	}
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
			s.markFailed(markCtx, rowID, "rule_failed")
			s.logError("rule evaluate failed", deliveryID, eventType, "rule_failed", repoName, err.Error(), time.Since(startedAt).Milliseconds())
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
		// 有事件时带出事件类型与事件 ID：delivery 行 ↔ 事件可互相检索定位。
		if res.Event != nil {
			attrs = append(attrs, "event_kind", res.Event.Kind)
			attrs = append(attrs, "event_id", res.Event.ID)
		}
		// 乱序丢弃与未处理动作是"处理了但没产生通知"的两类常见原因，
		// 带出布尔字段避免排障时把正常入库误判为通知丢失。
		if res.StaleDiscarded {
			attrs = append(attrs, "stale_discarded", true)
		}
		if res.UnhandledAction {
			attrs = append(attrs, "unhandled_action", true)
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

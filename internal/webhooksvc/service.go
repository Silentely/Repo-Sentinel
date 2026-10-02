// Package webhooksvc 承载 GitHub Webhook 的业务管线：
// 验签后的负载 → 规范化 → 实时通知决策 → WebhookDelivery 状态机。
// 从 httpapi 抽出，使 HTTP 层只负责请求/响应适配，不再编排领域流程。
package webhooksvc

import (
	"context"
	"log/slog"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// Evaluator 实时通知评估器（聚合器或引擎均可实现）。
type Evaluator interface {
	Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error
}

// Service 处理验签后的 Webhook 负载并更新投递状态。
type Service struct {
	Store  store.Store
	Logger *slog.Logger
	// Evaluator 实时通知决策器；nil 时回退内置 rules.Engine。
	Evaluator Evaluator
	// GitHub 可选；用于拉取 PR Diff 与发表 Review 评论。
	GitHub *githubx.AppClient
	// AI 可选；默认 rules.Engine 的安全告警分诊客户端。
	AI *ai.Client
	// Background 后台任务生命周期；关闭时由 App 取消。
	Background context.Context
	// OnFailed 可选指标回调：规范化或规则评估失败时触发（与 notify 的 OnSent 同模式，
	// 避免 webhooksvc 反向依赖 httpapi）。
	OnFailed func()
	// OnBroadcast 可选广播回调：Webhook 成功入库或状态流转时触发实时推流。
	OnBroadcast func(topic, resource, resourceID string)
	// SlowThreshold 慢处理判定阈值；<=0 时用默认 slowWebhookThreshold。
	SlowThreshold time.Duration
	// ReviewDebounceDelay PR 审查防抖窗口（默认 60s；<=0 时使用默认值；测试中可设为毫秒级）。
	ReviewDebounceDelay time.Duration
	// reviews 跟踪在途审查任务：同头互斥与停机排空（见 reviewTracker）。
	reviews   *reviewTracker
	debouncer *reviewDebouncer
}

// slowWebhookThreshold 单条 webhook 处理的慢阈值：超过说明规范化/评估路径存在
// 阻塞（数据库抖动、外部调用），以 Warn 留痕便于定位。
const slowWebhookThreshold = 5 * time.Second

// webhookProcessTimeout 单条 webhook 后台处理超时下限：
// 数据库挂起或外部依赖（AI 分诊等评估路径）变慢时，超时释放 32 并发槽位，
// 避免槽位被永久占用导致积压恶化。状态标记使用脱离取消的 markCtx，不受此超时影响。
// 实际预算取该值与「AI 配置超时 + 余量」的较大者（见 processBudget），
// 保证分诊调用预算不被处理预算截断，配置即强制。
const webhookProcessTimeout = 60 * time.Second

// webhookProcessMargin 处理预算在 AI 配置超时之上预留的非 AI 管线余量
// （规范化、状态标记等），避免 AI 恰好用满超时时整条处理被掐断。
const webhookProcessMargin = 10 * time.Second

// processBudget 计算单条 webhook 处理预算：以 webhookProcessTimeout 为下限，
// 若 AI 分诊配置的超时更高则随之放宽。nil 客户端按系统下限处理。
func processBudget(aiClient *ai.Client, base time.Duration) time.Duration {
	if aiClient == nil {
		return base
	}
	budget := aiClient.EffectiveTimeout() + webhookProcessMargin
	if budget < base {
		return base
	}
	return budget
}

// MarkFailed 显式将投递标记为失败（支持外部在生命周期取消或槽位耗尽时调用，避免行残留 accepted）。
func (s *Service) MarkFailed(rowID, deliveryID, eventType, errorCode string) {
	s.markFailed(rowID, deliveryID, eventType, errorCode)
}

// markFailed 统一处理失败分支：标记投递失败（带语义化错误码）、记录失败指标回调。
// 标记失败会让行残留 accepted/中间态，影响状态机与重放判断，必须留痕。
// deliveryID/eventType 与 logError 对齐：排查按 GitHub delivery_id 检索时不致漏掉该条 Warn。
func (s *Service) markFailed(rowID, deliveryID, eventType, errorCode string) {
	markCtx, markCancel := s.markContext()
	defer markCancel()
	if err := s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryFailed, errorCode); err != nil && s.Logger != nil {
		s.Logger.Warn("webhook mark failed status error",
			"delivery_id", deliveryID, "event_type", eventType, "error", err.Error(), "error_code", "webhook_mark_failed_status_error")
	}
	if s.OnFailed != nil {
		s.OnFailed()
	}
}

// markContext 为终态标记（MarkProcessed / MarkFailed）派生独立的短超时 context。
// 即使传入的 ctx（Background 生命周期）正在优雅关闭（已 cancel），状态机终态收敛依然必须尽力落库；
// 但又不能无限期挂死（数据库断连时），因此用 context.WithoutCancel 脱离原 context 取消链，并绑定 5 秒兜底超时。
func (s *Service) markContext() (context.Context, context.CancelFunc) {
	return context.WithTimeout(context.WithoutCancel(s.baseContext()), 5*time.Second)
}

// slowThreshold 返回慢处理判定阈值；配置 <=0 时回退默认值。
func (s *Service) slowThreshold() time.Duration {
	if s.SlowThreshold > 0 {
		return s.SlowThreshold
	}
	return slowWebhookThreshold
}

// Process 驱动单条 webhook 的处理管线：规范化 → 规则评估 → 状态推进。
// 必须在独立的 context（通常是 Background 生命周期）下运行，且已占用并发槽位。
func (s *Service) Process(rowID, eventType, deliveryID string, body []byte) {
	ctx := s.Background
	if ctx == nil {
		// 误装配（Background 未注入）：行将永久残留 accepted，必须留痕而非静默返回。
		if s.Logger != nil {
			s.Logger.Warn("webhook process skipped: background context missing",
				"delivery_id", deliveryID, "event_type", eventType, "error_code", "background_missing")
		}
		return
	}
	// 单条处理带超时预算：Background 为应用生命周期 context，无 Deadline；
	// 挂起时超时释放并发槽位（见 webhookProcessTimeout / processBudget 注释）。
	processCtx, processCancel := context.WithTimeout(ctx, processBudget(s.AI, webhookProcessTimeout))
	defer processCancel()
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
	// 关闭期间 Background 已取消：状态标记必须脱离取消，且预算从标记时刻起算（见 markContext）。
	proc := &normalizer.Processor{Store: s.Store, Logger: s.Logger}
	res, err := proc.Process(processCtx, eventType, deliveryID, body)
	if err != nil {
		s.markFailed(rowID, deliveryID, eventType, "normalize_failed")
		// 规范化失败时仓库信息尚未解析出来，repo 留空由调用方从日志链路定位。
		s.logError("webhook normalize failed", deliveryID, eventType, "normalize_failed", "", err.Error(), time.Since(startedAt).Milliseconds())
		return
	}
	if res.Repository != nil {
		repoName = res.Repository.FullName
	}
	if res.Event != nil && s.OnBroadcast != nil {
		s.OnBroadcast("events.created", "event", res.Event.ID)
	}
	s.maybeTriggerAICodeReview(res, body)

	if res.Event != nil && !res.SuppressNotify {
		var err error
		if s.Evaluator != nil {
			err = s.Evaluator.Evaluate(processCtx, res, repoName)
		} else {
			err = (&rules.Engine{Store: s.Store, AI: s.AI, Logger: s.Logger, GitHub: s.GitHub}).Evaluate(processCtx, res, repoName)
		}
		if err != nil {
			// 通知已丢：状态必须可查，标记为失败而不是 processed。
			s.markFailed(rowID, deliveryID, eventType, "rule_failed")
			s.logError("rule evaluate failed", deliveryID, eventType, "rule_failed", repoName, err.Error(), time.Since(startedAt).Milliseconds())
			return
		}
	}
	markCtx, markCancel := s.markContext()
	defer markCancel()
	if err := s.Store.WebhookDeliveries().MarkProcessed(markCtx, rowID, store.DeliveryProcessed, ""); err != nil {
		// 标记失败会让 delivery 行残留 accepted/中间态，影响状态机与重放判断，
		// 与 markFailed 失败同级别留痕，否则该行永久卡在 accepted 且无迹可查。
		if s.Logger != nil {
			s.Logger.Warn(
				"webhook mark processed failed",
				"delivery_id", deliveryID,
				"event_type", eventType,
				"repo", repoName,
				"error_code", "webhook_mark_processed_failed",
				"error", err.Error(),
			)
		}
	}
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
		// 成功路径降为 Debug：高流量仓库（Actions 批量事件）下每条 webhook 都 Info
		// 会刷屏，排查时调级即可；accepted（Info）与 failed（Warn）保留。
		s.Logger.Debug("github webhook processed", attrs...)
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

// StopReviews 进入审查停机排空状态：拒绝登记新的审查任务。
// App.Close 在 WaitReviews 之前调用：先拒绝新任务，再等待在途任务清空，
// 保证排空等待期间不会再有新任务并发登记后写已关闭的数据库。
func (s *Service) StopReviews() {
	if s.reviews != nil {
		s.reviews.stop()
	}
	if s.debouncer != nil {
		s.debouncer.stop()
	}
}

func (s *Service) tracker() *reviewTracker {
	if s.reviews != nil {
		return s.reviews
	}
	s.reviews = &reviewTracker{}
	return s.reviews
}

func (s *Service) getDebouncer() *reviewDebouncer {
	if s.debouncer != nil {
		return s.debouncer
	}
	s.debouncer = newReviewDebouncer()
	return s.debouncer
}

// WaitReviews 等待在途的 PR 审查任务清空或直到传入 context 超时/取消。
func (s *Service) WaitReviews(ctx context.Context) error {
	return s.reviews.wait(ctx)
}

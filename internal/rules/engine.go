package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"io"
	"log/slog"
	urlpkg "net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Engine 决定是否实时通知并写入 Outbox。
type Engine struct {
	Store store.Store
	// AI 可选；nil 或未启用时不进行安全告警分诊。
	AI *ai.Client
	// Logger 可选；分诊参与度与降级留痕。
	Logger *slog.Logger
	// GitHub 可选；用于拉取 CI Job 失败详情等上下文。
	GitHub *githubx.AppClient
}

// aiBudgetKeyAndLimit 返回当日预算设置键与预算上限（美分）。
func (e *Engine) aiBudgetKeyAndLimit() (string, int) {
	todayKey := "ai_budget:" + time.Now().UTC().Format("2006-01-02")
	budgetLimit := 500 // 默认 500 美分 ($5.00)
	if raw := os.Getenv("REPOSENTINEL_AI_DAILY_BUDGET_CENTS"); raw != "" {
		if v, err := strconv.Atoi(raw); err == nil && v > 0 {
			budgetLimit = v
		}
	}
	return todayKey, budgetLimit
}

// aiBudgetExceeded 只读检查当日 AI 预算是否已耗尽（不产生消耗）。
// 读失败时 fail closed（返回 true）避免预算系统异常时额度失控；
// 当日尚无用量（ErrNotFound）视为未超限。
func (e *Engine) aiBudgetExceeded(ctx context.Context) bool {
	if e.Store == nil {
		return false
	}
	todayKey, budgetLimit := e.aiBudgetKeyAndLimit()
	setting, err := e.Store.Settings().Get(ctx, todayKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false
		}
		if e.Logger != nil {
			e.Logger.Warn("ai budget check failed, failing closed", "error", err.Error())
		}
		return true
	}
	if len(setting.ValueJSON) == 0 {
		return false
	}
	exceeded, err := aiBudgetExceededFromJSON(setting.ValueJSON, budgetLimit)
	if err != nil {
		if e.Logger != nil {
			e.Logger.Warn("ai budget payload malformed, failing closed", "error", err.Error())
		}
		return true
	}
	return exceeded
}

// aiBudgetExceededFromJSON 判定当日预算 JSON 是否已超限：已置位熔断或累计成本达到上限。
func aiBudgetExceededFromJSON(valueJSON []byte, budgetLimit int) (bool, error) {
	if len(valueJSON) == 0 {
		return false, nil
	}
	var raw struct {
		CostEstCents int  `json:"cost_est_cents"`
		IsThrottled  any  `json:"is_throttled"`
		Throttled    bool `json:"throttled"`
	}
	if err := json.Unmarshal(valueJSON, &raw); err != nil {
		return false, err
	}
	return raw.Throttled || parseAIBool(raw.IsThrottled) || raw.CostEstCents >= budgetLimit, nil
}

// recordAIUsage 在 AI 调用实际执行后累加当日用量（失败调用不计费）。
// 熔断置位由累加语句内的成本比较完成，下一轮 aiBudgetExceeded 读取生效。
func (e *Engine) recordAIUsage(ctx context.Context, estTokens int, estCostCents int) {
	if e.Store == nil {
		return
	}
	todayKey, budgetLimit := e.aiBudgetKeyAndLimit()
	if _, err := e.Store.Settings().UpdateAIBudgetUsageAtomic(ctx, todayKey, estTokens, estCostCents, budgetLimit); err != nil {
		if e.Logger != nil {
			e.Logger.Warn("ai budget record failed", "error_code", "ai_budget_record_failed", "error", err.Error())
		}
	}
}

// parseAIBool 归一化 SQLite/Postgres 双方言返回的 is_throttled 值。
func parseAIBool(v any) bool {
	switch val := v.(type) {
	case bool:
		return val
	case float64:
		return val != 0
	case int64:
		return val != 0
	case int:
		return val != 0
	case string:
		return val == "true" || val == "1" || val == "t"
	case []byte:
		s := string(val)
		return s == "true" || s == "1" || s == "t"
	default:
		return false
	}
}

// logNotifySkipped 记录"事件已入库但未产生实时通知"的决策留痕（Debug）：
// 静默路径包括抑制、能力开关关闭与不在实时通知范围，排查漏通知时不再盲猜。
func (e *Engine) logNotifySkipped(res normalizer.Result, repoFullName, reason string) {
	if e.Logger == nil || res.Event == nil {
		return
	}
	e.Logger.Debug("notification skipped",
		"event_id", res.Event.ID, "kind", res.Event.Kind, "action", res.Event.Action,
		"repo", repoFullName, "reason", reason)
}

// Evaluate 根据规范化结果创建通知。
func (e *Engine) Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error {
	if e == nil {
		return errors.New("rules: engine is nil")
	}
	if res.Event == nil || res.SuppressNotify || res.Event.SuppressNotification {
		e.logNotifySkipped(res, repoFullName, "suppressed")
		return nil
	}
	// 能力开关兜底：全局功能 + 仓库级开关；存量/间隙事件也不能外发。
	if !allowsEventKind(ctx, e.Store, res.Repository, res.Event.Kind) {
		e.logNotifySkipped(res, repoFullName, "capability_off")
		return nil
	}
	if !shouldNotifyRealtime(res.Event) {
		e.logNotifySkipped(res, repoFullName, "not_realtime")
		return nil
	}
	if e.Store == nil {
		return errors.New("rules: engine store is required")
	}
	channels, err := e.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	if !hasSubscribedChannel(channels, res.Event.Kind) {
		e.logNotifySkipped(res, repoFullName, "no_subscriber")
		return nil
	}
	title, body, htmlURL := renderMessage(res.Event, repoFullName)
	branch := ExtractEventBranch(res.Event)
	repoID := ""
	if res.Event.RepositoryID != nil {
		repoID = *res.Event.RepositoryID
	} else if res.Repository != nil {
		repoID = res.Repository.ID
	}
	isMuted, muteReason := CheckEmergencyMute(ctx, e.Store, repoID)

	// 全部订阅渠道都会被过滤拦截或免打扰时不作 AI 分析：省下无效费用与等待。
	if !isMuted && hasReceivingChannel(channels, res.Event, repoFullName, branch) {
		// 安全告警分诊：新告警附带影响分析与处理建议；失败保持原文，不阻塞入库。
		// 是否有接收渠道的检查并入 triageAnalysis，与参与度日志归并一处。
		if analysis := e.triageAnalysis(ctx, res.Event, repoFullName, channels); analysis != "" {
			body = body + "\n────────────────\n🤖 告警分析\n" + htmlpkg.EscapeString(analysis)
		}
		// release 更新速览：新 release 附带智能翻译要点；失败降级原文链接，不阻塞入库。
		if summary := e.releaseAnalysis(ctx, res.Event, repoFullName, channels); summary != "" {
			body = body + "\n────────────────\n🤖 更新速览\n" + htmlpkg.EscapeString(summary)
		}
		// Actions 失败归因诊断：构建失败时附带 AI 智能归因与排查建议；失败保持原文，不阻塞入库。
		if diagnosis := e.workflowFailureAnalysis(ctx, res.Event, repoFullName, channels); diagnosis != "" {
			body = body + "\n────────────────\n🤖 故障诊断\n" + htmlpkg.EscapeString(diagnosis)
		}
		// Issue 智能分析与首响建议：新 Issue 附带分类、要素完整度与维护者回复建议
		if issueTriage := e.issueAnalysis(ctx, res.Event, repoFullName, channels); issueTriage != "" {
			body = body + "\n────────────────\n🤖 Issue 智能分析与回复建议\n" + htmlpkg.EscapeString(issueTriage)
		}
	}
	var chatOpsToken string
	if res.Event.Kind == store.WorkflowRunKind && store.IsFailureConclusion(res.Event.WorkflowConclusion) && res.Event.WorkflowRunID != nil && res.Event.RepositoryID != nil {
		runIDStr := strconv.FormatInt(*res.Event.WorkflowRunID, 10)
		if tokenID, err := CreateChatOpsToken(ctx, e.Store, "workflow_rerun", *res.Event.RepositoryID, runIDStr, "", 2*time.Hour); err == nil {
			chatOpsToken = tokenID
		}
	}

	for _, ch := range channels {
		// 渠道未订阅该事件类型时跳过。
		if !ch.Enabled || !ch.AcceptsKind(res.Event.Kind) {
			continue
		}
		// 免打扰：若渠道开启了忽略机器人且当前事件触发者为机器人，
		// 仅过滤常规 Issue 与 PR 事件；安全告警与 Actions 工作流严格豁免。
		if ShouldSuppressBotEvent(ch, res.Event) {
			e.logNotifySkipped(res, repoFullName, "bot_suppressed")
			continue
		}
		// 渠道精细化路由（仓库通配符、分支通配符、严重度门槛）
		if !MatchChannelFilter(ch, res.Event, repoFullName, branch) {
			e.logNotifySkipped(res, repoFullName, "channel_filter_mismatch")
			continue
		}
		bodyJSON := map[string]any{
			"event_id": res.Event.ID, "kind": res.Event.Kind, "action": res.Event.Action,
			"repository": repoFullName,
		}
		// 紧急静音激活时，持久化审计记录进 outbox（独立键命名空间，状态为 suppressed）
		if isMuted {
			e.logNotifySkipped(res, repoFullName, "emergency_mute")
			suppressedIdem := fmt.Sprintf("suppressed|%s|%s", res.Event.ID, ch.ID)
			now := time.Now().UTC()
			if _, err := e.Store.Outbox().Create(ctx, store.NotificationOutbox{
				ID:               ulid.Make().String(),
				ChannelID:        ch.ID,
				EventID:          &res.Event.ID,
				IdempotencyKey:   suppressedIdem,
				Status:           store.OutboxSuppressed,
				SuppressedReason: muteReason,
				NextAttemptAt:    now,
				Title:            title,
				BodyText:         body,
				HTMLURL:          htmlURL,
				BodyJSON:         bodyJSON,
				ParseMode:        "HTML",
			}); err != nil && !errors.Is(err, store.ErrConflict) {
				return err
			}
			continue
		}

		idem := idempotencyKey(ch.ID, res.Event.ID, "realtime")
		nextAttempt := time.Now().UTC()
		action, resumeAt := DecideQuietHours(ch, res.Event, nextAttempt)
		if action == ActionDeferQuietHours {
			nextAttempt = resumeAt
		}
		if chatOpsToken != "" {
			bodyJSON["chatops_token"] = chatOpsToken
			bodyJSON["chatops_action"] = "workflow_rerun"
		}
		if _, err := e.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &res.Event.ID,
			IdempotencyKey: idem, Status: store.OutboxPending, NextAttemptAt: nextAttempt,
			Title: title, BodyText: body, HTMLURL: htmlURL, BodyJSON: bodyJSON,
			ParseMode: "HTML",
		}); err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	}
	return nil
}

// ShouldSuppressBotEvent 是所有通知路径共享的渠道级机器人免打扰规则：
// 实时通知、合并聚合、超频摘要与定期报告（digest）统一复用，避免各路径各自实现导致口径漂移。
// 安全告警与 Actions 等事件保持现有豁免，仅过滤 Issue/PR 这类工作项事件。
func ShouldSuppressBotEvent(ch store.NotificationChannel, ev *store.Event) bool {
	return ev != nil && ch.IgnoreBots && ev.SenderIsBot &&
		(ev.Kind == store.WorkItemKindIssue || ev.Kind == store.WorkItemKindPR)
}

// hasReceivingChannel 判定是否存在「既订阅该事件类型、又不会被机器人免打扰过滤」的启用渠道。
// 全部渠道都会被过滤时无需发起 AI 分析：省下无效费用与最长一个 AI 超时的等待。
func hasReceivingChannel(channels []store.NotificationChannel, ev *store.Event, repoFullName, branch string) bool {
	for _, ch := range channels {
		if ch.Enabled && ch.AcceptsKind(ev.Kind) && !ShouldSuppressBotEvent(ch, ev) && MatchChannelFilter(ch, ev, repoFullName, branch) {
			return true
		}
	}
	return false
}

// allowsEventKind 判定全局功能 + 仓库能力是否放行该类型事件。
// 与 normalizer 采集门禁语义一致，两道防线保证「关闭即生效」。
// res.Repository 为 nil（聚合器 flush 单事件回放）时仍检查全局功能开关。
func allowsEventKind(ctx context.Context, st store.Store, repo *store.Repository, kind string) bool {
	if st != nil && !store.KindFeatureEnabled(ctx, st.Settings(), kind) {
		return false
	}
	return store.RepoAllowsKind(repo, kind)
}

func shouldNotifyRealtime(ev *store.Event) bool {
	switch ev.Kind {
	case store.WorkItemKindIssue:
		switch ev.Action {
		case "opened", "reopened", "closed":
			return true
		}
	case store.WorkItemKindPR:
		switch ev.Action {
		case "opened", "reopened", "closed", "merged", "ready_for_review", "converted_to_draft":
			// draft 变化进摘要；ready_for_review 实时
			return ev.Action != "converted_to_draft"
		}
	case store.WorkflowRunKind:
		if ev.Action == "recovered" {
			return true
		}
		return store.IsFailureConclusion(ev.WorkflowConclusion)
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		// 安全告警不论 action（创建/忽略/严重度变化等）一律实时通知，避免遗漏风险。
		return true
	case store.StarKind:
		switch ev.Action {
		case "created", "deleted":
			return true
		}
	case store.WatchKind:
		if ev.Action == "started" {
			return true
		}
	case store.ReleaseKind:
		// release 发布事件实时通知；其余 action 不通知。
		return ev.Action == "published"
	}
	return false
}

// hasSubscribedChannel 判定是否存在启用且订阅该事件类型的渠道。
// 用于 AI 分诊等有外部成本的前置判断：无接收方时不调用。
func hasSubscribedChannel(channels []store.NotificationChannel, kind string) bool {
	for _, ch := range channels {
		if ch.Enabled && ch.AcceptsKind(kind) {
			return true
		}
	}
	return false
}

// triageAnalysis 生成新安全告警的告警分析；未启用、非新告警、无订阅渠道或调用失败时返回空串。
// 返回空串时调用方保持原通知正文，AI 慢或不可用绝不影响通知入库。
// 参与度留痕（Logger 注入时）：skipped 记录未参与原因（triage_not_enabled / not_new_alert /
// no_subscribed_channel），used 记录分诊成功，fallback 记录失败、空输出或格式不达标
// （reason=ai_error / empty_analysis / format_invalid，附错误详情）；fallback/used 携带与
// ai 层调用日志相同的 req_id，可还原完整调用链。
func (e *Engine) triageAnalysis(ctx context.Context, ev *store.Event, repo string, channels []store.NotificationChannel) string {
	// 仅安全告警参与分诊；其余事件类型静默返回，不产生 skipped 日志噪声。
	if !isSecurityAlertKind(ev.Kind) {
		return ""
	}
	skip := func(reason string) string {
		if e.Logger != nil {
			e.Logger.Info("triage ai skipped", "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "reason", reason)
		}
		return ""
	}
	if e.AI == nil || !e.AI.IsTriageEnabled() {
		return skip("triage_not_enabled")
	}
	if !isNewSecurityAlert(ev) {
		return skip("not_new_alert")
	}
	// 无订阅渠道时不发起 AI 请求，避免无效费用；原因同样留痕。
	if !hasSubscribedChannel(channels, ev.Kind) {
		return skip("no_subscribed_channel")
	}
	if e.aiBudgetExceeded(ctx) {
		if e.Logger != nil {
			e.Logger.Warn("triage ai fallback", "event_id", ev.ID, "reason", "ai_budget_throttled")
		}
		return ""
	}
	// 为本次 AI 决策注入请求关联 ID：参与度日志与 ai 层调用日志共用同一 req_id。
	ctx, reqID := ai.EnsureRequestID(ctx)
	// 外层预算 = 配置的请求超时：分诊等待时长与用户配置一致，AI 慢时通知最迟
	// 延迟配置超时后降级原文，不会被更短的硬编码上限截断。
	ctx, cancel := context.WithTimeout(ctx, e.AI.EffectiveTimeout())
	defer cancel()
	start := time.Now()
	analysis, err := e.AI.TriageAlert(ctx, *ev, repo)
	duration := time.Since(start)
	if err != nil || strings.TrimSpace(analysis) == "" {
		if e.Logger != nil {
			reason := "empty_analysis"
			if err != nil {
				reason = "ai_error"
			}
			attrs := []any{"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds(), "reason", reason}
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}
			e.Logger.Warn("triage ai fallback", attrs...)
		}
		return ""
	}
	// AI 调用已实际执行（无传输错误）：计入当日用量；失败调用不计费。
	e.recordAIUsage(ctx, 300, 1)
	// 格式护栏：提示词要求首行以「影响：」开头，不达标视为低质输出，降级保持原正文。
	firstLine := analysis
	if i := strings.IndexByte(analysis, '\n'); i >= 0 {
		firstLine = analysis[:i]
	}
	if !strings.HasPrefix(strings.TrimSpace(firstLine), "影响：") {
		if e.Logger != nil {
			e.Logger.Warn("triage ai fallback",
				"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action,
				"duration_ms", duration.Milliseconds(), "reason", "format_invalid")
		}
		return ""
	}
	if e.Logger != nil {
		e.Logger.Info("triage ai used", "req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds())
	}
	return analysis
}

// releaseAnalysis 生成新 release 的更新速览；未启用、非 release、无订阅渠道或失败时返回空串。
// 返回空串时调用方保持原通知正文（原文链接兜底）。
// 外层预算 = 配置的请求超时（e.AI.EffectiveTimeout）：等待时长与用户配置一致，
// AI 慢时通知最迟延迟配置超时后降级原文，不会被更短的硬编码上限截断。
// 参与度留痕与 triageAnalysis 同款：skipped（release_summary_not_enabled /
// no_subscribed_channel）、used、fallback（reason=ai_error / empty_analysis）。
func (e *Engine) releaseAnalysis(ctx context.Context, ev *store.Event, repo string, channels []store.NotificationChannel) string {
	// 仅 release 事件参与；其余事件类型静默返回，不产生 skipped 日志噪声。
	if ev.Kind != store.ReleaseKind {
		return ""
	}
	skip := func(reason string) string {
		if e.Logger != nil {
			e.Logger.Info("release ai skipped", "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "reason", reason)
		}
		return ""
	}
	if e.AI == nil || !e.AI.IsReleaseSummaryEnabled() {
		return skip("release_summary_not_enabled")
	}
	if !hasSubscribedChannel(channels, ev.Kind) {
		return skip("no_subscribed_channel")
	}
	if e.aiBudgetExceeded(ctx) {
		return skip("ai_budget_throttled")
	}
	ctx, reqID := ai.EnsureRequestID(ctx)
	ctx, cancel := context.WithTimeout(ctx, e.AI.EffectiveTimeout())
	defer cancel()
	start := time.Now()
	tag := store.PayloadString(ev.PayloadSummary, "tag_name")
	notes := store.PayloadString(ev.PayloadSummary, "notes")
	summary, err := e.AI.ReleaseSummary(ctx, repo, tag, notes, ev.HTMLURL)
	duration := time.Since(start)
	if err != nil || strings.TrimSpace(summary) == "" {
		if e.Logger != nil {
			reason := "empty_analysis"
			if err != nil {
				reason = "ai_error"
			}
			attrs := []any{"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds(), "reason", reason}
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}
			e.Logger.Warn("release ai fallback", attrs...)
		}
		return ""
	}
	// AI 调用已实际执行（无传输错误）：计入当日用量；失败调用不计费。
	e.recordAIUsage(ctx, 500, 2)
	if e.Logger != nil {
		e.Logger.Info("release ai used", "req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds())
	}
	return summary
}

// workflowFailureAnalysis 生成 Actions 失败运行的 AI 归因诊断；未启用、非失败运行、
// 无订阅渠道或调用失败时返回空串。返回空串时调用方保持原通知正文，AI 慢或不可用
// 绝不影响通知入库。由独立的「CI 诊断」开关（IsFailureAnalysisEnabled）控制，与
// 安全告警分诊开关互不影响。
// 参与度留痕与 triageAnalysis 同款：skipped（failure_analysis_not_enabled /
// no_subscribed_channel）、used、fallback（reason=ai_error / empty_analysis / format_invalid）。
// 仅失败的 Actions 运行参与诊断；其余事件类型静默返回，不产生 skipped 日志噪声
// （类型检查必须先于 AI 开关检查，与 triageAnalysis/releaseAnalysis 的约定一致，
// 否则每条实时事件都会刷一条 skipped Info 日志）。
func (e *Engine) workflowFailureAnalysis(ctx context.Context, ev *store.Event, repo string, channels []store.NotificationChannel) string {
	if ev.Kind != store.WorkflowRunKind || !store.IsFailureConclusion(ev.WorkflowConclusion) {
		return ""
	}
	skip := func(reason string) string {
		if e.Logger != nil {
			e.Logger.Info("workflow failure ai skipped", "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "reason", reason)
		}
		return ""
	}
	if e.AI == nil || !e.AI.IsFailureAnalysisEnabled() {
		return skip("failure_analysis_not_enabled")
	}
	if !hasSubscribedChannel(channels, ev.Kind) {
		return skip("no_subscribed_channel")
	}
	if e.aiBudgetExceeded(ctx) {
		return skip("ai_budget_throttled")
	}

	ctx, reqID := ai.EnsureRequestID(ctx)
	ctx, cancel := context.WithTimeout(ctx, e.AI.EffectiveTimeout())
	defer cancel()
	start := time.Now()

	workflowName := store.PayloadString(ev.PayloadSummary, "workflow_name")
	if workflowName == "" {
		workflowName = ev.Title
	}
	branch := store.PayloadString(ev.PayloadSummary, "head_branch")

	// 尝试通过 GitHub API 提取失败的 Job / Step 信息作为诊断依据。
	// 提取失败仅留 Warn 并降级为「未知步骤」：诊断仍会发起，但可观测——
	// 运维能区分「本来就没有失败步骤」与「上下文提取失败」。
	var failedSteps []string
	if e.GitHub != nil && ev.WorkflowRunID != nil && *ev.WorkflowRunID != 0 && e.Store != nil {
		owner, repoName := store.SplitFullName(repo)
		if owner != "" && repoName != "" {
			var token string
			repoRec, err := e.Store.Repositories().GetByFullName(ctx, repo)
			switch {
			case err != nil:
				if e.Logger != nil {
					e.Logger.Warn("workflow failure context: get repository failed", "req_id", reqID, "repo", repo, "run_id", *ev.WorkflowRunID, "error", err.Error())
				}
			case repoRec.InstallationID == nil:
				// 无安装上下文：匿名拉取公开仓即可，不视为故障。
			default:
				if instID, perr := strconv.ParseInt(*repoRec.InstallationID, 10, 64); perr != nil || instID <= 0 {
					// 安装 ID 非法：同上按无安装上下文降级。
				} else if tok, tokErr := e.GitHub.InstallationToken(ctx, instID); tokErr != nil {
					if e.Logger != nil {
						e.Logger.Warn("workflow failure context: resolve installation token failed", "req_id", reqID, "repo", repo, "run_id", *ev.WorkflowRunID, "error", tokErr.Error())
					}
				} else {
					token = tok
				}
			}
			jobsCtx, cancelJobs := context.WithTimeout(ctx, 5*time.Second)
			jobs, err := e.GitHub.ListWorkflowJobs(jobsCtx, token, owner, repoName, *ev.WorkflowRunID)
			cancelJobs()
			if err != nil {
				if e.Logger != nil {
					e.Logger.Warn("workflow failure context: list workflow jobs failed", "req_id", reqID, "repo", repo, "run_id", *ev.WorkflowRunID, "error", err.Error())
				}
			} else {
				for _, j := range jobs {
					if store.IsFailureConclusion(j.Conclusion) {
						jobHasFailedStep := false
						for _, s := range j.Steps {
							if store.IsFailureConclusion(s.Conclusion) {
								failedSteps = append(failedSteps, j.Name+" / "+s.Name)
								jobHasFailedStep = true
							}
						}
						if !jobHasFailedStep {
							failedSteps = append(failedSteps, j.Name)
						}
					}
				}
			}
		}
	}

	diagnosis, err := e.AI.DiagnoseWorkflowFailure(ctx, repo, workflowName, branch, ev.WorkflowConclusion, failedSteps)
	duration := time.Since(start)
	if err != nil || strings.TrimSpace(diagnosis) == "" {
		if e.Logger != nil {
			reason := "empty_analysis"
			if err != nil {
				reason = "ai_error"
			}
			attrs := []any{"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds(), "reason", reason}
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}
			e.Logger.Warn("workflow failure ai fallback", attrs...)
		}
		return ""
	}

	// AI 调用已实际执行（无传输错误）：计入当日用量；失败调用不计费。
	e.recordAIUsage(ctx, 500, 2)

	// 质量防护：输出必须以「诊断：」开头
	if !strings.HasPrefix(strings.TrimSpace(diagnosis), "诊断：") {
		if e.Logger != nil {
			e.Logger.Warn("workflow failure ai fallback",
				"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action,
				"duration_ms", duration.Milliseconds(), "reason", "format_invalid")
		}
		return ""
	}

	if e.Logger != nil {
		e.Logger.Info("workflow failure ai used", "req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds())
	}
	return diagnosis
}

// issueAnalysis 生成新创建 Issue 的 AI 智能分诊与首响回复建议；未启用、非新 Issue、无订阅频道或调用失败时返回空串。
func (e *Engine) issueAnalysis(ctx context.Context, ev *store.Event, repo string, channels []store.NotificationChannel) string {
	if ev.Kind != store.WorkItemKindIssue || ev.Action != "opened" {
		return ""
	}
	skip := func(reason string) string {
		if e.Logger != nil {
			e.Logger.Info("issue triage ai skipped", "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "reason", reason)
		}
		return ""
	}
	if e.AI == nil || !e.AI.IsIssueTriageEnabled() {
		return skip("issue_triage_not_enabled")
	}
	if !hasSubscribedChannel(channels, ev.Kind) {
		return skip("no_subscribed_channel")
	}
	if e.aiBudgetExceeded(ctx) {
		return skip("ai_budget_throttled")
	}

	parentCtx := ctx
	ctx, reqID := ai.EnsureRequestID(parentCtx)
	aiCtx, cancel := context.WithTimeout(ctx, e.AI.EffectiveTimeout())
	defer cancel()
	start := time.Now()

	// 正文为归一化时写入事件载荷的副本（已按 textutil.MaxBodyTextBytes 截断）。
	// 键缺失表示该事件创建时未采集正文，留痕以便区分「无正文」与「正文为空」。
	bodyText := store.PayloadString(ev.PayloadSummary, "body")
	if _, ok := ev.PayloadSummary["body"]; !ok && e.Logger != nil {
		e.Logger.Warn("issue triage body not captured in event payload",
			"req_id", reqID, "event_id", ev.ID, "reason", "body_not_captured")
	}
	res, err := e.AI.TriageIssue(aiCtx, repo, ev.Title, ev.Actor, bodyText)
	duration := time.Since(start)
	if err != nil || res == nil {
		if e.Logger != nil {
			reason := "empty_analysis"
			if err != nil {
				reason = "ai_error"
			}
			attrs := []any{"req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds(), "reason", reason}
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}
			e.Logger.Warn("issue triage ai fallback", attrs...)
		}
		return ""
	}
	// AI 调用已实际执行（无传输错误）：计入当日用量；失败调用不计费。
	e.recordAIUsage(ctx, 400, 2)
	if e.Logger != nil {
		e.Logger.Info("issue triage ai used", "req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds())
	}
	// 将分诊结果持久化，供 Web 界面查看与首响应建议复制
	if e.Store != nil && ev.RepositoryID != nil && ev.SubjectNumber != nil {
		if wi, err := e.Store.WorkItems().GetByRepoNumber(parentCtx, *ev.RepositoryID, int(*ev.SubjectNumber)); err == nil && wi.ID != "" {
			if raw, err := json.Marshal(res); err == nil {
				if _, err := e.Store.Settings().Upsert(parentCtx, store.SystemSetting{
					ID:        ulid.Make().String(),
					Key:       "ai.issue_triage." + wi.ID,
					ValueJSON: raw,
					UpdatedAt: time.Now().UTC(),
					UpdatedBy: "issue_triage",
				}); err != nil && e.Logger != nil {
					e.Logger.Warn("issue triage result persist failed",
						"req_id", reqID, "event_id", ev.ID, "work_item_id", wi.ID,
						"error_code", "issue_triage_persist_failed", "error", err.Error())
				}
			}
		}
		e.maybeAutoLabelIssue(parentCtx, repo, int(*ev.SubjectNumber), res)
	}
	return ai.FormatIssueTriage(res)
}

func (e *Engine) maybeAutoLabelIssue(ctx context.Context, repoFullName string, issueNumber int, res *ai.IssueTriageResult) {
	if e.Store == nil || e.GitHub == nil || res == nil {
		return
	}
	setting, err := e.Store.Settings().Get(ctx, "ai.auto_label_enabled")
	if err != nil {
		return
	}
	var enabled bool
	if err := json.Unmarshal(setting.ValueJSON, &enabled); err != nil || !enabled {
		return
	}

	var rawLabels []string
	if res.Category != "" {
		rawLabels = append(rawLabels, res.Category)
	}
	rawLabels = append(rawLabels, res.Labels...)
	labels := githubx.FilterAndMapLabels(rawLabels)
	if len(labels) == 0 {
		return
	}
	if !githubx.ShouldAutoLabelIssue(res.Category, labels) {
		return
	}

	target := res.Category
	// 存量兼容：迁移前回执键为 github_label:<repo>:<num>:<category>，
	// 命中说明该 Issue 已打标，跳过以免升级后重复调用打标接口。
	if _, legacyErr := e.Store.Settings().Get(ctx, store.LegacyAutoLabelReceiptKey(repoFullName, issueNumber, target)); legacyErr == nil {
		if e.Logger != nil {
			e.Logger.Debug("issue auto-label skipped on legacy receipt", "repo", repoFullName, "issue", issueNumber)
		}
		return
	}
	if err := ExecuteWithReceipt(ctx, e.Store.Settings(), "auto_label", repoFullName, strconv.Itoa(issueNumber), "v1", target, func() (string, error) {
		parts := strings.SplitN(repoFullName, "/", 2)
		if len(parts) != 2 {
			return "", MarkPreSendError(fmt.Errorf("invalid repo full name: %s", repoFullName))
		}
		owner, repo := parts[0], parts[1]

		repoRec, err := e.Store.Repositories().GetByFullName(ctx, repoFullName)
		if err != nil || repoRec.InstallationID == nil {
			return "", MarkPreSendError(fmt.Errorf("repo or installation missing: %w", err))
		}
		instID, err := strconv.ParseInt(*repoRec.InstallationID, 10, 64)
		if err != nil || instID <= 0 {
			return "", MarkPreSendError(fmt.Errorf("invalid installation id: %v", repoRec.InstallationID))
		}
		token, err := e.GitHub.InstallationToken(ctx, instID)
		if err != nil || token == "" {
			return "", MarkPreSendError(fmt.Errorf("failed to get installation token: %w", err))
		}

		bgCtx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		if err := e.GitHub.AddIssueLabels(bgCtx, token, owner, repo, issueNumber, labels); err != nil {
			if e.Logger != nil {
				e.Logger.Warn("issue auto-label failed", "repo", repoFullName, "issue", issueNumber, "labels", labels, "error", err.Error())
			}
			return "", err
		}
		return strings.Join(labels, ","), nil
	}); err != nil && e.Logger != nil {
		// 回执写入异常等：留痕，避免副作用被静默丢弃。
		e.Logger.Warn("issue auto-label skipped", "repo", repoFullName, "issue", issueNumber,
			"error_code", "side_effect_receipt_error", "error", err.Error())
	}
}

// isSecurityAlertKind 判定事件是否为安全告警类型（分诊仅针对告警）。
func isSecurityAlertKind(kind string) bool {
	switch kind {
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		return true
	}
	return false
}

// isNewSecurityAlert 判定是否为「新产生」的安全告警（创建/打开/重新打开）。
// 忽略与修复等终态事件无需 AI 分诊。
func isNewSecurityAlert(ev *store.Event) bool {
	if !isSecurityAlertKind(ev.Kind) {
		return false
	}
	switch ev.Action {
	case "created", "opened", "reopened":
		return true
	}
	return false
}

func renderMessage(ev *store.Event, repo string) (title, body, htmlURL string) {
	if ev == nil {
		return "", "", ""
	}
	statusEmoji, statusLabel := statusDisplay(ev)
	// 标题把状态放最前，通知列表/推送预览第一眼就能看出打开还是关闭。
	// Outbox.Title 保存纯文本（供各渠道纯文本/Markdown 标题与管理台列表使用，不带 HTML 实体）。
	title = statusEmoji + " " + statusLabel + "｜" + ev.Title

	var b strings.Builder
	b.Grow(512)
	b.WriteString("<b>")
	b.WriteString(htmlpkg.EscapeString(title))
	b.WriteString("</b>\n────────────────\n")

	// 状态置顶：正文第二行再次强化，避免只看字段时漏掉。
	b.WriteString(statusEmoji)
	b.WriteString(" <b>状态：")
	b.WriteString(htmlpkg.EscapeString(statusLabel))
	b.WriteString("</b>\n")

	repo = strings.TrimSpace(repo)
	if repo != "" {
		b.WriteString("📦 仓库：<code>")
		b.WriteString(htmlpkg.EscapeString(repo))
		b.WriteString("</code>\n")
	}

	if ev.SubjectNumber != nil && ev.Kind != store.ReleaseKind {
		b.WriteString("🔢 编号：#")
		b.WriteString(strconv.FormatInt(*ev.SubjectNumber, 10))
		b.WriteString("\n")
	}

	// release 事件用版本号（tag_name）替代编号行。
	if tag := store.PayloadString(ev.PayloadSummary, "tag_name"); tag != "" {
		b.WriteString("🏷️ 版本：<code>")
		b.WriteString(htmlpkg.EscapeString(tag))
		b.WriteString("</code>\n")
	}

	b.WriteString("📋 类型：")
	b.WriteString(htmlpkg.EscapeString(store.KindDisplayName(ev.Kind)))
	b.WriteString("\n")

	if ev.Actor != "" {
		b.WriteString("👤 操作者：")
		b.WriteString(htmlpkg.EscapeString(ev.Actor))
		b.WriteString("\n")
	}

	// 安全告警 — 严重度中文化 + 规则/依赖
	if ev.Severity != "" {
		sevEmoji := severityEmoji(ev.Severity)
		b.WriteString(sevEmoji)
		b.WriteString(" 严重度：")
		b.WriteString(htmlpkg.EscapeString(severityDisplayName(ev.Severity)))
		b.WriteString("\n")
	}
	if rule := store.PayloadString(ev.PayloadSummary, "rule_or_dependency"); rule != "" {
		b.WriteString("🛡️ 规则：")
		b.WriteString(htmlpkg.EscapeString(rule))
		b.WriteString("\n")
	}

	// Workflow 结论已并入「状态」行，正文只补充分支与工作流名。
	if branch := store.PayloadString(ev.PayloadSummary, "head_branch"); branch != "" {
		b.WriteString("🌿 分支：<code>")
		b.WriteString(htmlpkg.EscapeString(branch))
		b.WriteString("</code>\n")
	}
	if wfName := store.PayloadString(ev.PayloadSummary, "workflow_name"); wfName != "" {
		b.WriteString("⚙️ 工作流：")
		b.WriteString(htmlpkg.EscapeString(wfName))
		b.WriteString("\n")
	}

	if labels := payloadStringSlice(ev.PayloadSummary, "labels"); len(labels) > 0 {
		b.WriteString("🏷️ 标签：")
		b.WriteString(htmlpkg.EscapeString(strings.Join(labels, ", ")))
		b.WriteString("\n")
	}

	if assignees := payloadStringSlice(ev.PayloadSummary, "assignees"); len(assignees) > 0 {
		b.WriteString("👥 指派：")
		b.WriteString(htmlpkg.EscapeString(strings.Join(assignees, ", ")))
		b.WriteString("\n")
	}

	if ms := store.PayloadString(ev.PayloadSummary, "milestone"); ms != "" {
		b.WriteString("📅 里程碑：")
		b.WriteString(htmlpkg.EscapeString(ms))
		b.WriteString("\n")
	}

	if !ev.OccurredAt.IsZero() {
		b.WriteString("⏰ 时间：")
		b.WriteString(ev.OccurredAt.UTC().Format("2006-01-02 15:04 UTC"))
		b.WriteString("\n")
	}

	if link := SafeHTTPURL(ev.HTMLURL); link != "" {
		b.WriteString("────────────────\n<a href=\"")
		b.WriteString(htmlpkg.EscapeString(link))
		b.WriteString("\">")
		b.WriteString(store.GitHubViewLabel)
		b.WriteString("</a>")
		htmlURL = link
	}
	return title, b.String(), htmlURL
}

func SafeHTTPURL(raw string) string {
	link := strings.TrimSpace(raw)
	if link == "" {
		return ""
	}
	u, err := urlpkg.Parse(link)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return ""
	}
	return link
}

// statusDisplay / workflowConclusionEmoji / actionDisplayName / severityDisplayName /
// eventEmoji / severityEmoji / isDraft 已移至 display.go（与 digest 报告共用，
// 避免多份映射表漂移）；EventStatusLabel 为无 emoji 的中文状态标签，供报告预览复用。

// payloadStringSlice 从 PayloadSummary 安全读取字符串切片字段。
func payloadStringSlice(m map[string]any, key string) []string {
	if m == nil {
		return nil
	}
	raw, ok := m[key].([]any)
	if !ok {
		return nil
	}
	out := make([]string, 0, len(raw))
	for _, v := range raw {
		if s, ok := v.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func idempotencyKey(channelID, eventID, variant string) string {
	h := sha256.New()
	_, _ = io.WriteString(h, channelID)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, eventID)
	_, _ = io.WriteString(h, "|")
	_, _ = io.WriteString(h, variant)
	var sum [sha256.Size]byte
	h.Sum(sum[:0])
	var buf [sha256.Size * 2]byte
	hex.Encode(buf[:], sum[:])
	return string(buf[:])
}

// RenderMessage formats an event and repository into notification title, HTML body, and HTML URL.
func RenderMessage(ev *store.Event, repo string) (title, body, htmlURL string) {
	return renderMessage(ev, repo)
}

type DryRunChannelResult struct {
	ChannelID   string `json:"channel_id"`
	ChannelType string `json:"channel_type"`
	Name        string `json:"name"`
	Matched     bool   `json:"matched"`
	Reason      string `json:"reason,omitempty"`
	ParseMode   string `json:"parse_mode"`
}

type DryRunResult struct {
	EventKind      string                `json:"event_kind"`
	Action         string                `json:"action"`
	Repository     string                `json:"repository"`
	Title          string                `json:"title"`
	BodyText       string                `json:"body_text"`
	HTMLURL        string                `json:"html_url"`
	MatchedRules   []string              `json:"matched_rules"`
	ChannelResults []DryRunChannelResult `json:"channel_results"`
	IsMuted        bool                  `json:"is_muted"`
	MuteReason     string                `json:"mute_reason,omitempty"`
}

func (e *Engine) DryRun(ctx context.Context, res normalizer.Result, repoFullName string, customChannels []store.NotificationChannel) (DryRunResult, error) {
	if res.Event == nil {
		return DryRunResult{}, errors.New("event is required for dry run")
	}
	title, body, htmlURL := renderMessage(res.Event, repoFullName)
	branch := ExtractEventBranch(res.Event)
	repoID := ""
	if res.Event.RepositoryID != nil {
		repoID = *res.Event.RepositoryID
	} else if res.Repository != nil {
		repoID = res.Repository.ID
	}

	isMuted := false
	muteReason := ""
	if e != nil && e.Store != nil {
		isMuted, muteReason = CheckEmergencyMute(ctx, e.Store, repoID)
	}

	channels := customChannels
	if len(channels) == 0 && e != nil && e.Store != nil {
		var err error
		channels, err = e.Store.Channels().List(ctx)
		if err != nil {
			return DryRunResult{}, err
		}
	}

	var matchedRules []string
	if e != nil && e.Store != nil && allowsEventKind(ctx, e.Store, res.Repository, res.Event.Kind) {
		matchedRules = append(matchedRules, "capability_allowed")
	}
	if shouldNotifyRealtime(res.Event) {
		matchedRules = append(matchedRules, "realtime_evaluation_pass")
	}
	if isMuted {
		matchedRules = append(matchedRules, "emergency_mute_active")
	}

	var channelResults []DryRunChannelResult
	for _, ch := range channels {
		parseMode := "HTML"
		matched := false
		reason := "matched"

		if !ch.Enabled {
			reason = "channel_disabled"
		} else if !ch.AcceptsKind(res.Event.Kind) {
			reason = "event_kind_not_subscribed"
		} else if ShouldSuppressBotEvent(ch, res.Event) {
			reason = "bot_suppressed"
		} else if !MatchChannelFilter(ch, res.Event, repoFullName, branch) {
			reason = "channel_filter_mismatch"
		} else if isMuted {
			reason = "emergency_mute: " + muteReason
		} else {
			matched = true
			matchedRules = append(matchedRules, fmt.Sprintf("channel:%s:delivered", ch.ChannelType))
		}

		channelResults = append(channelResults, DryRunChannelResult{
			ChannelID:   ch.ID,
			ChannelType: ch.ChannelType,
			Name:        ch.Name,
			Matched:     matched,
			Reason:      reason,
			ParseMode:   parseMode,
		})
	}

	return DryRunResult{
		EventKind:      res.Event.Kind,
		Action:         res.Event.Action,
		Repository:     repoFullName,
		Title:          title,
		BodyText:       body,
		HTMLURL:        htmlURL,
		MatchedRules:   matchedRules,
		ChannelResults: channelResults,
		IsMuted:        isMuted,
		MuteReason:     muteReason,
	}, nil
}

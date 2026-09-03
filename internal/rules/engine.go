package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	htmlpkg "html"
	"log/slog"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
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
	title, body, htmlURL := renderMessage(res.Event, repoFullName)
	// 安全告警分诊：新告警附带影响分析与处理建议；失败保持原文，不阻塞入库。
	// 是否有接收渠道的检查并入 triageAnalysis，与参与度日志归并一处。
	if analysis := e.triageAnalysis(ctx, res.Event, repoFullName, channels); analysis != "" {
		body = body + "\n────────────────\n🤖 告警分析\n" + htmlpkg.EscapeString(analysis)
	}
	// release 更新速览：新 release 附带智能翻译要点；失败降级原文链接，不阻塞入库。
	if summary := e.releaseAnalysis(ctx, res.Event, repoFullName, channels); summary != "" {
		body = body + "\n────────────────\n🤖 更新速览\n" + htmlpkg.EscapeString(summary)
	}
	for _, ch := range channels {
		// 渠道未订阅该事件类型时跳过。
		if !ch.Enabled || !ch.AcceptsKind(res.Event.Kind) {
			continue
		}
		idem := idempotencyKey(ch.ID, res.Event.ID, "realtime")
		if _, err := e.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &res.Event.ID,
			IdempotencyKey: idem, Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, HTMLURL: htmlURL, BodyJSON: map[string]any{
				"event_id": res.Event.ID, "kind": res.Event.Kind, "action": res.Event.Action,
			},
			ParseMode: "HTML",
		}); err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
	}
	return nil
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
	if e.Logger != nil {
		e.Logger.Info("release ai used", "req_id", reqID, "event_id", ev.ID, "kind", ev.Kind, "action", ev.Action, "duration_ms", duration.Milliseconds())
	}
	return summary
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
	title = fmt.Sprintf("%s %s｜%s", statusEmoji, statusLabel, htmlpkg.EscapeString(ev.Title))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")

	// 状态置顶：正文第二行再次强化，避免只看字段时漏掉。
	b.WriteString(fmt.Sprintf("%s <b>状态：%s</b>\n", statusEmoji, htmlpkg.EscapeString(statusLabel)))

	repo = strings.TrimSpace(repo)
	if repo != "" {
		b.WriteString(fmt.Sprintf("📦 仓库：<code>%s</code>\n", htmlpkg.EscapeString(repo)))
	}

	if ev.SubjectNumber != nil && ev.Kind != store.ReleaseKind {
		b.WriteString(fmt.Sprintf("🔢 编号：#%d\n", *ev.SubjectNumber))
	}

	// release 事件用版本号（tag_name）替代编号行。
	if tag := store.PayloadString(ev.PayloadSummary, "tag_name"); tag != "" {
		b.WriteString(fmt.Sprintf("🏷️ 版本：<code>%s</code>\n", htmlpkg.EscapeString(tag)))
	}

	b.WriteString(fmt.Sprintf("📋 类型：%s\n", htmlpkg.EscapeString(store.KindDisplayName(ev.Kind))))

	if ev.Actor != "" {
		b.WriteString(fmt.Sprintf("👤 操作者：%s\n", htmlpkg.EscapeString(ev.Actor)))
	}

	// 安全告警 — 严重度中文化 + 规则/依赖
	if ev.Severity != "" {
		sevEmoji := severityEmoji(ev.Severity)
		b.WriteString(fmt.Sprintf("%s 严重度：%s\n", sevEmoji, htmlpkg.EscapeString(severityDisplayName(ev.Severity))))
	}
	if rule := store.PayloadString(ev.PayloadSummary, "rule_or_dependency"); rule != "" {
		b.WriteString(fmt.Sprintf("🛡️ 规则：%s\n", htmlpkg.EscapeString(rule)))
	}

	// Workflow 结论已并入「状态」行，正文只补充分支与工作流名。
	if branch := store.PayloadString(ev.PayloadSummary, "head_branch"); branch != "" {
		b.WriteString(fmt.Sprintf("🌿 分支：<code>%s</code>\n", htmlpkg.EscapeString(branch)))
	}
	if wfName := store.PayloadString(ev.PayloadSummary, "workflow_name"); wfName != "" {
		b.WriteString(fmt.Sprintf("⚙️ 工作流：%s\n", htmlpkg.EscapeString(wfName)))
	}

	if labels := payloadStringSlice(ev.PayloadSummary, "labels"); len(labels) > 0 {
		b.WriteString(fmt.Sprintf("🏷️ 标签：%s\n", htmlpkg.EscapeString(strings.Join(labels, ", "))))
	}

	if assignees := payloadStringSlice(ev.PayloadSummary, "assignees"); len(assignees) > 0 {
		b.WriteString(fmt.Sprintf("👥 指派：%s\n", htmlpkg.EscapeString(strings.Join(assignees, ", "))))
	}

	if ms := store.PayloadString(ev.PayloadSummary, "milestone"); ms != "" {
		b.WriteString(fmt.Sprintf("📅 里程碑：%s\n", htmlpkg.EscapeString(ms)))
	}

	if !ev.OccurredAt.IsZero() {
		b.WriteString(fmt.Sprintf("⏰ 时间：%s\n", ev.OccurredAt.UTC().Format("2006-01-02 15:04 UTC")))
	}

	if link := strings.TrimSpace(ev.HTMLURL); link != "" {
		b.WriteString("────────────────\n")
		b.WriteString(fmt.Sprintf("<a href=\"%s\">%s</a>", htmlpkg.EscapeString(link), store.GitHubViewLabel))
		htmlURL = link
	}
	return title, b.String(), htmlURL
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
	sum := sha256.Sum256([]byte(channelID + "|" + eventID + "|" + variant))
	return hex.EncodeToString(sum[:])
}

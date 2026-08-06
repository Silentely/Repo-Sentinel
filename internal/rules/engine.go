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

// aiTriageTimeout 分诊调用预算上限：告警通知应尽快入库，AI 慢则放弃分诊。
const aiTriageTimeout = 15 * time.Second

// Evaluate 根据规范化结果创建通知。
func (e *Engine) Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error {
	if res.Event == nil || res.SuppressNotify || res.Event.SuppressNotification {
		return nil
	}
	// 能力开关兜底：全局功能 + 仓库级开关；存量/间隙事件也不能外发。
	if !allowsEventKind(ctx, e.Store, res.Repository, res.Event.Kind) {
		return nil
	}
	if !shouldNotifyRealtime(res.Event) {
		return nil
	}
	channels, err := e.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	title, body, htmlURL := renderMessage(res.Event, repoFullName)
	// 安全告警分诊：新告警附带 AI 影响分析与处理建议；失败保持原文，不阻塞入库。
	// 是否有接收渠道的检查并入 triageAnalysis，与参与度日志归并一处。
	if analysis := e.triageAnalysis(ctx, res.Event, repoFullName, channels); analysis != "" {
		body = body + "\n────────────────\n🤖 AI 分析\n" + htmlpkg.EscapeString(analysis)
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

// triageAnalysis 生成新安全告警的 AI 分析；未启用、非新告警、无订阅渠道或调用失败时返回空串。
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
	ctx, cancel := context.WithTimeout(ctx, aiTriageTimeout)
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
	statusEmoji, statusLabel := statusDisplay(ev)
	// 标题把状态放最前，通知列表/推送预览第一眼就能看出打开还是关闭。
	title = fmt.Sprintf("%s %s｜%s", statusEmoji, statusLabel, htmlpkg.EscapeString(ev.Title))

	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")

	// 状态置顶：正文第二行再次强化，避免只看字段时漏掉。
	b.WriteString(fmt.Sprintf("%s <b>状态：%s</b>\n", statusEmoji, htmlpkg.EscapeString(statusLabel)))

	if repo != "" {
		b.WriteString(fmt.Sprintf("📦 仓库：<code>%s</code>\n", htmlpkg.EscapeString(repo)))
	}

	if ev.SubjectNumber != nil {
		b.WriteString(fmt.Sprintf("🔢 编号：#%d\n", *ev.SubjectNumber))
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

	if ev.HTMLURL != "" {
		b.WriteString("────────────────\n")
		b.WriteString(fmt.Sprintf("<a href=\"%s\">🔗 在 GitHub 中查看</a>", htmlpkg.EscapeString(ev.HTMLURL)))
		htmlURL = ev.HTMLURL
	}
	return title, b.String(), htmlURL
}

// statusDisplay 返回事件的状态 emoji 与中文标签，供标题/正文一眼识别开闭与结论。
func statusDisplay(ev *store.Event) (emoji, label string) {
	switch ev.Kind {
	case store.WorkItemKindIssue:
		switch ev.Action {
		case "opened":
			return "🟢", "已打开"
		case "reopened":
			return "🔁", "重新打开"
		case "closed":
			return "⚫", "已关闭"
		}
	case store.WorkItemKindPR:
		if isDraft(ev) && ev.Action != "ready_for_review" && ev.Action != "merged" && ev.Action != "closed" {
			return "📝", "草稿"
		}
		switch ev.Action {
		case "opened":
			return "🟢", "已打开"
		case "reopened":
			return "🔁", "重新打开"
		case "closed":
			return "⚫", "已关闭"
		case "merged":
			return "🟣", "已合并"
		case "ready_for_review":
			return "👀", "待审核"
		case "converted_to_draft":
			return "📝", "转为草稿"
		}
	case store.WorkflowRunKind:
		if ev.Action == "recovered" {
			return "🟢", "已恢复"
		}
		switch ev.WorkflowConclusion {
		case "success":
			return "✅", "成功"
		case "failure", "startup_failure":
			return "❌", "失败"
		case "cancelled":
			return "⏹️", "已取消"
		case "timed_out":
			return "⏱️", "超时"
		case "action_required":
			return "🔔", "需处理"
		case "skipped":
			return "⏭️", "已跳过"
		default:
			if store.IsFailureConclusion(ev.WorkflowConclusion) {
				return "❌", "失败"
			}
			return "⚙️", "已完成"
		}
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		switch ev.Action {
		case "created", "opened", "reopened":
			return severityEmoji(ev.Severity), "新告警"
		case "fixed", "resolved":
			return "✅", "已修复"
		case "dismissed", "closed":
			return "🔇", "已忽略"
		case "auto_dismissed":
			return "🔇", "自动忽略"
		default:
			if ev.Severity != "" {
				return severityEmoji(ev.Severity), "告警更新"
			}
			return "🛡️", "告警更新"
		}
	case store.StarKind:
		switch ev.Action {
		case "created":
			return "⭐", "已收藏"
		case "deleted":
			return "💔", "取消收藏"
		}
	case store.WatchKind:
		if ev.Action == "started" {
			return "👀", "已关注"
		}
	}

	// 通用回退：按 action 语义猜测
	switch {
	case strings.Contains(ev.Action, "reopen"):
		return "🔁", "重新打开"
	case ev.Action == "closed" || ev.Action == "fixed" || ev.Action == "dismissed" || ev.Action == "resolved":
		return "⚫", "已关闭"
	case ev.Action == "opened" || ev.Action == "created":
		return "🟢", "已打开"
	default:
		if ev.Action != "" {
			return eventEmoji(ev), actionDisplayName(ev.Action)
		}
		return eventEmoji(ev), "有更新"
	}
}

// actionDisplayName 将 GitHub action 转为简短中文（通用回退）。
func actionDisplayName(action string) string {
	switch action {
	case "opened":
		return "已打开"
	case "closed":
		return "已关闭"
	case "reopened":
		return "重新打开"
	case "merged":
		return "已合并"
	case "created":
		return "已创建"
	case "updated", "edited":
		return "已更新"
	case "completed":
		return "已完成"
	case "recovered":
		return "已恢复"
	case "dismissed":
		return "已忽略"
	case "fixed", "resolved":
		return "已修复"
	case "ready_for_review":
		return "待审核"
	case "converted_to_draft":
		return "转为草稿"
	default:
		return action
	}
}

// severityDisplayName 严重度中文。
func severityDisplayName(severity string) string {
	switch strings.ToLower(severity) {
	case "critical":
		return "严重 (critical)"
	case "high", "error":
		return "高 (high)"
	case "medium", "warning":
		return "中 (medium)"
	case "low", "note":
		return "低 (low)"
	default:
		return severity
	}
}

// eventEmoji 根据事件类型和状态返回对应的 emoji。
func eventEmoji(ev *store.Event) string {
	switch {
	// 安全告警 — 按严重度区分
	case ev.Kind == store.AlertKindSecretScanning:
		return "🔑"
	case ev.Kind == store.AlertKindCodeScanning && ev.Severity == "error":
		return "🔴"
	case ev.Kind == store.AlertKindCodeScanning && ev.Severity == "warning":
		return "🟠"
	case ev.Kind == store.AlertKindCodeScanning:
		return "🔎"
	case ev.Kind == store.AlertKindDependabot && ev.Severity == "critical":
		return "🚨"
	case ev.Kind == store.AlertKindDependabot:
		return "📦"

	// PR 状态细分
	case ev.Kind == store.WorkItemKindPR && ev.Action == "merged":
		return "🟣"
	case ev.Kind == store.WorkItemKindPR && isDraft(ev):
		return "📝"
	case ev.Kind == store.WorkItemKindPR:
		return "🔀"

	// star/watch 事件
	case ev.Kind == store.StarKind:
		return "⭐"
	case ev.Kind == store.WatchKind:
		return "👀"

	// Issue 状态
	case ev.Kind == store.WorkItemKindIssue && ev.Action == "opened":
		return "🐛"
	case ev.Kind == store.WorkItemKindIssue:
		return "📋"

	// WorkflowRun 结论细分
	case ev.Kind == store.WorkflowRunKind && ev.Action == "recovered":
		return "🟢"
	case ev.Kind == store.WorkflowRunKind && ev.WorkflowConclusion == "success":
		return "✅"
	case ev.Kind == store.WorkflowRunKind && ev.WorkflowConclusion == "cancelled":
		return "⏹️"
	case ev.Kind == store.WorkflowRunKind && ev.WorkflowConclusion == "timed_out":
		return "⏱️"
	case ev.Kind == store.WorkflowRunKind:
		return "❌"

	// 通用回退
	case strings.Contains(ev.Action, "reopen"):
		return "🔁"
	case ev.Action == "closed" || ev.Action == "fixed" || ev.Action == "dismissed" || ev.Action == "resolved":
		return "✅"
	default:
		return "📋"
	}
}

// severityEmoji 根据严重度返回对应 emoji。
func severityEmoji(severity string) string {
	switch severity {
	case "critical":
		return "🚨"
	case "high", "error":
		return "🔴"
	case "medium", "warning":
		return "🟠"
	case "low", "note":
		return "🟡"
	default:
		return "⚠️"
	}
}

// workflowConclusionEmoji 根据 workflow 结论返回对应 emoji。
func workflowConclusionEmoji(conclusion string) string {
	switch conclusion {
	case "success":
		return "✅"
	case "failure":
		return "❌"
	case "cancelled":
		return "⏹️"
	case "timed_out":
		return "⏱️"
	case "action_required":
		return "🔔"
	case "skipped":
		return "⏭️"
	case "startup_failure":
		return "💥"
	default:
		return "📊"
	}
}

// isDraft 从 PayloadSummary 安全读取 draft 字段。
func isDraft(ev *store.Event) bool {
	if ev.PayloadSummary == nil {
		return false
	}
	if v, ok := ev.PayloadSummary["draft"].(bool); ok {
		return v
	}
	return false
}

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

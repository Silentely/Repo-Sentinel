package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	htmlpkg "html"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Engine 决定是否实时通知并写入 Outbox。
type Engine struct {
	Store store.Store
}

// Evaluate 根据规范化结果创建通知。
func (e *Engine) Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error {
	if res.Event == nil || res.SuppressNotify || res.Event.SuppressNotification {
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
	for _, ch := range channels {
		if !ch.Enabled {
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
		}); err != nil && err != store.ErrConflict {
			return err
		}
	}
	return nil
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
	case "workflow_run":
		if ev.Action == "recovered" {
			return true
		}
		return normalizerIsFailure(ev.WorkflowConclusion)
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		switch ev.Action {
		case "created", "reopened", "opened", "fixed", "resolved", "dismissed",
			"auto_dismissed", "appear_in_branch":
			return true
		default:
			// 严重程度变化等
			return true
		}
	}
	return false
}

func normalizerIsFailure(c string) bool {
	switch c {
	case "failure", "timed_out", "cancelled", "action_required", "startup_failure":
		return true
	default:
		return false
	}
}

func renderMessage(ev *store.Event, repo string) (title, body, htmlURL string) {
	emoji := eventEmoji(ev)
	title = fmt.Sprintf("%s %s", emoji, htmlpkg.EscapeString(ev.Title))

	var b strings.Builder
	// 标题行
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")

	// 仓库
	if repo != "" {
		b.WriteString(fmt.Sprintf("📦 仓库：<code>%s</code>\n", htmlpkg.EscapeString(repo)))
	}

	// 编号
	if ev.SubjectNumber != nil {
		b.WriteString(fmt.Sprintf("🔢 编号：#%d\n", *ev.SubjectNumber))
	}

	// 类型 / 操作
	b.WriteString(fmt.Sprintf("📋 类型：%s / %s\n", htmlpkg.EscapeString(ev.Kind), htmlpkg.EscapeString(ev.Action)))

	// 操作者
	if ev.Actor != "" {
		b.WriteString(fmt.Sprintf("👤 操作者：%s\n", htmlpkg.EscapeString(ev.Actor)))
	}

	// 安全告警 — 严重度 + 规则/依赖
	if ev.Severity != "" {
		sevEmoji := severityEmoji(ev.Severity)
		b.WriteString(fmt.Sprintf("%s 严重度：%s\n", sevEmoji, htmlpkg.EscapeString(ev.Severity)))
	}
	if rule := payloadString(ev.PayloadSummary, "rule_or_dependency"); rule != "" {
		b.WriteString(fmt.Sprintf("🛡️ 规则：%s\n", htmlpkg.EscapeString(rule)))
	}

	// Workflow 结论
	if ev.WorkflowConclusion != "" {
		conclusionEmoji := workflowConclusionEmoji(ev.WorkflowConclusion)
		b.WriteString(fmt.Sprintf("%s 结论：%s\n", conclusionEmoji, htmlpkg.EscapeString(ev.WorkflowConclusion)))
	}
	if branch := payloadString(ev.PayloadSummary, "head_branch"); branch != "" {
		b.WriteString(fmt.Sprintf("🌿 分支：<code>%s</code>\n", htmlpkg.EscapeString(branch)))
	}
	if wfName := payloadString(ev.PayloadSummary, "workflow_name"); wfName != "" {
		b.WriteString(fmt.Sprintf("⚙️ 工作流：%s\n", htmlpkg.EscapeString(wfName)))
	}

	// PR 草稿/合并状态
	if ev.Kind == store.WorkItemKindPR {
		if isDraft(ev) {
			b.WriteString("📝 状态：草稿\n")
		} else if ev.Action == "merged" {
			b.WriteString("🟣 状态：已合并\n")
		}
	}

	// Labels
	if labels := payloadStringSlice(ev.PayloadSummary, "labels"); len(labels) > 0 {
		b.WriteString(fmt.Sprintf("🏷️ 标签：%s\n", htmlpkg.EscapeString(strings.Join(labels, ", "))))
	}

	// Assignees
	if assignees := payloadStringSlice(ev.PayloadSummary, "assignees"); len(assignees) > 0 {
		b.WriteString(fmt.Sprintf("👥 指派：%s\n", htmlpkg.EscapeString(strings.Join(assignees, ", "))))
	}

	// Milestone
	if ms := payloadString(ev.PayloadSummary, "milestone"); ms != "" {
		b.WriteString(fmt.Sprintf("📅 里程碑：%s\n", htmlpkg.EscapeString(ms)))
	}

	// 事件时间
	if !ev.OccurredAt.IsZero() {
		b.WriteString(fmt.Sprintf("⏰ 时间：%s\n", ev.OccurredAt.UTC().Format("2006-01-02 15:04 UTC")))
	}

	// 分隔线 + 链接
	if ev.HTMLURL != "" {
		b.WriteString("────────────────\n")
		b.WriteString(fmt.Sprintf("<a href=\"%s\">🔗 在 GitHub 中查看</a>", htmlpkg.EscapeString(ev.HTMLURL)))
		htmlURL = ev.HTMLURL
	}
	return title, b.String(), htmlURL
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

	// Issue 状态
	case ev.Kind == store.WorkItemKindIssue && ev.Action == "opened":
		return "🐛"
	case ev.Kind == store.WorkItemKindIssue:
		return "📋"

	// WorkflowRun 结论细分
	case ev.Kind == "workflow_run" && ev.Action == "recovered":
		return "🟢"
	case ev.Kind == "workflow_run" && ev.WorkflowConclusion == "success":
		return "✅"
	case ev.Kind == "workflow_run" && ev.WorkflowConclusion == "cancelled":
		return "⏹️"
	case ev.Kind == "workflow_run" && ev.WorkflowConclusion == "timed_out":
		return "⏱️"
	case ev.Kind == "workflow_run":
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

// payloadString 从 PayloadSummary 安全读取字符串字段。
func payloadString(m map[string]any, key string) string {
	if m == nil {
		return ""
	}
	if v, ok := m[key].(string); ok {
		return v
	}
	return ""
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

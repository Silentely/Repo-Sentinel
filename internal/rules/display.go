package rules

import (
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// 本文件集中事件「状态中文 / emoji / 严重度中文」的展示映射，
// 供实时通知（engine.go）、聚合通知（aggregate.go）与定期报告（digest 包）复用，
// 避免多份 switch 表各自演进导致文案漂移。

// EventStatusLabel 返回事件的中文状态标签（不含 emoji，供摘要/报告预览行使用）。
// 与 statusDisplay 的 label 部分同源：PR 草稿判定、工作流结论、告警动作等
// 语义与实时通知完全一致，digest 包不再维护第二套映射。
func EventStatusLabel(ev *store.Event) string {
	switch ev.Kind {
	case store.WorkItemKindIssue:
		switch ev.Action {
		case "opened":
			return "已打开"
		case "reopened":
			return "重新打开"
		case "closed":
			return "已关闭"
		}
	case store.WorkItemKindPR:
		if isDraft(ev) && ev.Action != "ready_for_review" && ev.Action != "merged" && ev.Action != "closed" {
			return "草稿"
		}
		switch ev.Action {
		case "opened":
			return "已打开"
		case "reopened":
			return "重新打开"
		case "closed":
			return "已关闭"
		case "merged":
			return "已合并"
		case "ready_for_review":
			return "待审核"
		case "converted_to_draft":
			return "转为草稿"
		}
	case store.WorkflowRunKind:
		if ev.Action == "recovered" {
			return "已恢复"
		}
		return store.WorkflowConclusionLabel(ev.WorkflowConclusion)
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		switch ev.Action {
		case "created", "opened", "reopened":
			return "新告警"
		case "fixed", "resolved":
			return "已修复"
		case "dismissed", "closed":
			return "已忽略"
		case "auto_dismissed":
			return "自动忽略"
		case "withdrawn":
			return "已撤回"
		default:
			return "告警更新"
		}
	case store.StarKind:
		switch ev.Action {
		case "created":
			return "已收藏"
		case "deleted":
			return "取消收藏"
		}
	case store.WatchKind:
		if ev.Action == "started" {
			return "已关注"
		}
	case store.ReleaseKind:
		if ev.Action == "published" {
			return "新版本发布"
		}
	}

	// 通用回退：按 action 语义猜测
	switch {
	case strings.Contains(ev.Action, "reopen"):
		return "重新打开"
	case ev.Action == "closed" || ev.Action == "fixed" || ev.Action == "dismissed" || ev.Action == "resolved":
		return "已关闭"
	case ev.Action == "opened" || ev.Action == "created":
		return "已打开"
	default:
		if ev.Action != "" {
			return actionDisplayName(ev.Action)
		}
		return "有更新"
	}
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
		return workflowConclusionEmoji(ev.WorkflowConclusion), store.WorkflowConclusionLabel(ev.WorkflowConclusion)
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
		case "withdrawn":
			return "🛡️", "已撤回"
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
	case store.ReleaseKind:
		if ev.Action == "published" {
			return "🚀", "新版本发布"
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

// workflowConclusionEmoji 返回 Workflow 结论的 emoji（label 由 store.WorkflowConclusionLabel 提供）。
func workflowConclusionEmoji(conclusion string) string {
	switch conclusion {
	case "success":
		return "✅"
	case "failure", "startup_failure":
		return "❌"
	case "cancelled":
		return "⏹️"
	case "timed_out":
		return "⏱️"
	case "action_required":
		return "🔔"
	case "skipped":
		return "⏭️"
	default:
		if store.IsFailureConclusion(conclusion) {
			return "❌"
		}
		return "📊"
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
	case "withdrawn":
		return "已撤回"
	case "ready_for_review":
		return "待审核"
	case "converted_to_draft":
		return "转为草稿"
	case "published":
		return "发布"
	default:
		return action
	}
}

// severityDisplayName 严重度中文。
// 保留英文关键词便于与 GitHub 页面/API 原文对照，属刻意设计。
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

// KindEmoji 按事件类别返回统一 emoji（报告分组行与通用回退共用单一来源，
// 避免 digest 与 rules 各自维护类别 emoji 表导致漂移）。
func KindEmoji(kind string) string {
	switch kind {
	case store.WorkItemKindIssue:
		return "🐛"
	case store.WorkItemKindPR:
		return "🔀"
	case store.AlertKindDependabot:
		return "📦"
	case store.AlertKindCodeScanning:
		return "🔎"
	case store.AlertKindSecretScanning:
		return "🔑"
	case store.WorkflowRunKind:
		return "⚙️"
	case store.StarKind:
		return "⭐"
	case store.WatchKind:
		return "👀"
	case store.ReleaseKind:
		return "🚀"
	default:
		return "📋"
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

	// release 事件
	case ev.Kind == store.ReleaseKind:
		return "🚀"

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

package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"html"
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
	title, body := renderMessage(res.Event, repoFullName)
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		idem := idempotencyKey(ch.ID, res.Event.ID, "realtime")
		if _, err := e.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &res.Event.ID,
			IdempotencyKey: idem, Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, BodyJSON: map[string]any{
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

func renderMessage(ev *store.Event, repo string) (string, string) {
	emoji := "📋"
	switch {
	case ev.Kind == store.WorkItemKindIssue && ev.Action == "opened":
		emoji = "🐛"
	case strings.Contains(ev.Action, "reopen"):
		emoji = "🔁"
	case ev.Action == "closed" || ev.Action == "merged" || ev.Action == "fixed" || ev.Action == "dismissed" || ev.Action == "resolved":
		emoji = "✅"
	case ev.Kind == store.WorkItemKindPR:
		emoji = "🔀"
	case ev.Kind == store.AlertKindDependabot:
		emoji = "📦"
	case ev.Kind == store.AlertKindCodeScanning:
		emoji = "🔎"
	case ev.Kind == store.AlertKindSecretScanning:
		emoji = "🔑"
	case ev.Kind == "workflow_run" && ev.Action == "recovered":
		emoji = "🟢"
	case ev.Kind == "workflow_run":
		emoji = "❌"
	}
	title := fmt.Sprintf("%s %s", emoji, html.EscapeString(ev.Title))
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	if repo != "" {
		b.WriteString(fmt.Sprintf("仓库：<code>%s</code>\n", html.EscapeString(repo)))
	}
	b.WriteString(fmt.Sprintf("类型：%s / %s\n", html.EscapeString(ev.Kind), html.EscapeString(ev.Action)))
	if ev.Actor != "" {
		b.WriteString(fmt.Sprintf("操作者：%s\n", html.EscapeString(ev.Actor)))
	}
	if ev.Severity != "" {
		b.WriteString(fmt.Sprintf("严重度：%s\n", html.EscapeString(ev.Severity)))
	}
	if ev.WorkflowConclusion != "" {
		b.WriteString(fmt.Sprintf("结论：%s\n", html.EscapeString(ev.WorkflowConclusion)))
	}
	if ev.SubjectNumber != nil {
		b.WriteString(fmt.Sprintf("编号：#%d\n", *ev.SubjectNumber))
	}
	if ev.HTMLURL != "" {
		b.WriteString(fmt.Sprintf("<a href=\"%s\">在 GitHub 中打开</a>", html.EscapeString(ev.HTMLURL)))
	}
	return title, b.String()
}

func idempotencyKey(channelID, eventID, variant string) string {
	sum := sha256.Sum256([]byte(channelID + "|" + eventID + "|" + variant))
	return hex.EncodeToString(sum[:])
}

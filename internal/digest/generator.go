package digest

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	settingTimezone   = "admin.timezone"
	settingLocalTime  = "digest.local_time"
	settingSendEmpty  = "digest.send_empty"
	settingLastDigest = "digest.last_sent_date"
)

// Generator 每日摘要。
type Generator struct {
	Store store.Store
}

// RunOnce 若到达管理员本地发送时刻且当日未发送，则生成摘要通知。
func (g *Generator) RunOnce(ctx context.Context, now time.Time) error {
	tzName := "UTC"
	if s, err := g.Store.Settings().Get(ctx, settingTimezone); err == nil {
		var v string
		if json.Unmarshal(s.ValueJSON, &v) == nil && v != "" {
			tzName = v
		}
	}
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	localNow := now.In(loc)
	sendAt := "09:00"
	if s, err := g.Store.Settings().Get(ctx, settingLocalTime); err == nil {
		var v string
		if json.Unmarshal(s.ValueJSON, &v) == nil && v != "" {
			sendAt = v
		}
	}
	var hour, minute int
	if _, err := fmt.Sscanf(sendAt, "%d:%d", &hour, &minute); err != nil {
		hour, minute = 9, 0
	}
	// 仅在本地时刻到达后的一小时窗口内尝试，避免整点错过
	if localNow.Hour() < hour || (localNow.Hour() == hour && localNow.Minute() < minute) {
		return nil
	}
	if localNow.Hour() > hour+1 {
		return nil
	}
	dateKey := localNow.Format("2006-01-02")
	if s, err := g.Store.Settings().Get(ctx, settingLastDigest); err == nil {
		var last string
		if json.Unmarshal(s.ValueJSON, &last) == nil && last == dateKey {
			return nil
		}
	}

	// 收集近 24h 事件并按类别分组；全局关闭的功能不进入摘要。
	since := now.Add(-24 * time.Hour)
	events, err := g.Store.Events().ListSince(ctx, since, 500)
	if err != nil {
		return err
	}
	features := store.LoadFeatureFlags(ctx, g.Store.Settings())
	filtered := events[:0]
	for _, ev := range events {
		if features.AllowsKind(ev.Kind) {
			filtered = append(filtered, ev)
		}
	}
	events = filtered
	sendEmpty := false
	if s, err := g.Store.Settings().Get(ctx, settingSendEmpty); err == nil {
		_ = json.Unmarshal(s.ValueJSON, &sendEmpty)
	}
	if len(events) == 0 && !sendEmpty {
		return nil
	}

	title := fmt.Sprintf("📊 每日摘要 %s", dateKey)
	body := buildDigestBody(title, events)

	channels, err := g.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		// 渠道关闭每日汇总时跳过。
		if !ch.Enabled || !ch.DigestEnabled {
			continue
		}
		idem := fmt.Sprintf("digest|%s|%s", ch.ID, dateKey)
		_, err := g.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: map[string]any{"digest": true, "date": dateKey, "count": len(events)},
		})
		if err != nil && err != store.ErrConflict {
			return err
		}
	}
	raw, _ := json.Marshal(dateKey)
	_, _ = g.Store.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: settingLastDigest, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "system",
	})
	return nil
}

// buildDigestBody 构建分组格式的每日摘要正文。
func buildDigestBody(title string, events []store.Event) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")

	if len(events) == 0 {
		b.WriteString("🎉 过去 24 小时无新事件\n")
		return b.String()
	}

	b.WriteString(fmt.Sprintf("过去 24 小时共 %d 条事件\n", len(events)))
	b.WriteString("────────────────\n")

	// 按 kind 分组计数
	groups := make(map[string]int)
	for _, ev := range events {
		groups[ev.Kind]++
	}
	// 按数量降序排列
	type kv struct {
		kind  string
		count int
	}
	sorted := make([]kv, 0, len(groups))
	for k, v := range groups {
		sorted = append(sorted, kv{k, v})
	}
	sort.Slice(sorted, func(i, j int) bool { return sorted[i].count > sorted[j].count })
	for _, g := range sorted {
		b.WriteString(fmt.Sprintf("%s %s × %d\n", kindEmoji(g.kind), kindDisplayName(g.kind), g.count))
	}

	// 最近 5 条事件预览
	maxPreview := 5
	b.WriteString("────────────────\n")
	b.WriteString("最近活动：\n")
	for i, ev := range events {
		if i >= maxPreview {
			break
		}
		emoji := kindEmoji(ev.Kind)
		numStr := ""
		if ev.SubjectNumber != nil {
			numStr = fmt.Sprintf(" #%d", *ev.SubjectNumber)
		}
		b.WriteString(fmt.Sprintf("• %s%s %s\n", emoji, numStr, ev.Title))
	}

	return b.String()
}

// kindEmoji 根据事件类别返回 emoji。
func kindEmoji(kind string) string {
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
	case "workflow_run":
		return "⚙️"
	default:
		return "📋"
	}
}

// kindDisplayName 返回事件类别的中文显示名。
func kindDisplayName(kind string) string {
	switch kind {
	case store.WorkItemKindIssue:
		return "Issue"
	case store.WorkItemKindPR:
		return "PR"
	case store.AlertKindDependabot:
		return "Dependabot"
	case store.AlertKindCodeScanning:
		return "Code Scanning"
	case store.AlertKindSecretScanning:
		return "Secret Scanning"
	case "workflow_run":
		return "工作流"
	default:
		return kind
	}
}

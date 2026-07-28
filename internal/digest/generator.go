package digest

import (
	"context"
	"encoding/json"
	"fmt"
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

	// 收集近 24h 非实时类事件（简化：全部事件计数）
	since := now.Add(-24 * time.Hour)
	count, err := g.Store.Events().CountSince(ctx, since)
	if err != nil {
		return err
	}
	sendEmpty := false
	if s, err := g.Store.Settings().Get(ctx, settingSendEmpty); err == nil {
		_ = json.Unmarshal(s.ValueJSON, &sendEmpty)
	}
	if count == 0 && !sendEmpty {
		// 仍标记已检查日期，避免每小时空转？规格：无事件默认不发送；不写 last 以便次日再试
		return nil
	}

	title := fmt.Sprintf("📋 每日摘要 %s", dateKey)
	body := fmt.Sprintf("<b>%s</b>\n过去 24 小时事件 %d 条。\n请在仪表盘查看详情。", title, count)
	channels, err := g.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		idem := fmt.Sprintf("digest|%s|%s", ch.ID, dateKey)
		_, err := g.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: map[string]any{"digest": true, "date": dateKey, "count": count},
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

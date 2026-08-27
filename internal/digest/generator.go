package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	settingTimezone   = "admin.timezone"
	settingLocalTime  = "digest.local_time"
	settingSendEmpty  = "digest.send_empty"
	settingLastDigest = "digest.last_sent_date"
	// 周报/月报设置键。
	settingWeeklyEnabled  = "report.weekly_enabled"
	settingWeeklyDay      = "report.weekly_day"
	settingLastWeekly     = "report.last_weekly_date"
	settingMonthlyEnabled = "report.monthly_enabled"
	settingMonthlyDay     = "report.monthly_day"
	settingLastMonthly    = "report.last_monthly_date"
)

// 默认发送参数：周一发送周报、每月 1 日发送月报。
const (
	defaultWeeklyDay  = "monday"
	defaultMonthlyDay = 1
)

// minSummaryRunes AI 摘要质量下限：低于该长度的输出视为低质回退模板。
// 阈值保守（正常总结远高于此），主要拦截「OK」「无事件」类空泛输出。
const minSummaryRunes = 12

const (
	// dailyEventLimit 日报事件量上限；weeklyMonthlyEventLimit 周/月报事件量上限。
	// 与 ai 包 maxEventLines（200，摘要输入行数）存在隐含联动：拉取量大于输入量时
	// 超出部分仅进模板预览不进 AI 摘要，调整时同步评估。
	dailyEventLimit         = 500
	weeklyMonthlyEventLimit = 1000
	// maxPreviewEvents 正文「最近活动」预览条数。
	maxPreviewEvents = 5
)

// Generator 定期报告生成器：每日摘要 / 每周报告 / 每月报告。
type Generator struct {
	Store store.Store
	// AI 可选；nil 或未启用时不使用 AI 总结，回退模板正文。
	AI *ai.Client
	// Logger 可选；记账等尽力而为操作的失败留痕。
	Logger *slog.Logger
}

// RunOnce 每日摘要：到达管理员本地发送时刻且当日未发送时生成。
func (g *Generator) RunOnce(ctx context.Context, now time.Time) error {
	loc, ok := g.sendWindow(ctx, now)
	if !ok {
		return nil
	}
	localNow := now.In(loc)
	dateKey := localNow.Format("2006-01-02")
	if store.SettingString(ctx, g.Store.Settings(), settingLastDigest, "") == dateKey {
		return nil
	}

	since := now.Add(-24 * time.Hour)
	events, err := g.filteredEvents(ctx, since, dailyEventLimit)
	if err != nil {
		return err
	}
	if len(events) == 0 && !store.SettingBool(ctx, g.Store.Settings(), settingSendEmpty, false) {
		return nil
	}

	title := fmt.Sprintf("📊 每日摘要 %s", dateKey)
	body, aiUsed := g.reportBody(ctx, title, events, "过去 24 小时", now)
	return g.enqueue(ctx, settingLastDigest, "digest", dateKey, title, body, map[string]any{
		"digest": true, "date": dateKey, "count": len(events), "ai": aiUsed,
	})
}

// RunWeekly 每周报告：在配置的发送日到达发送时刻且本周未发送时生成。
// 周期为发送日起的最近 7 天，幂等键按发送日日期稳定。
func (g *Generator) RunWeekly(ctx context.Context, now time.Time) error {
	if !store.SettingBool(ctx, g.Store.Settings(), settingWeeklyEnabled, false) {
		return nil
	}
	loc, ok := g.sendWindow(ctx, now)
	if !ok {
		return nil
	}
	localNow := now.In(loc)
	wd, valid := parseWeekday(strings.ToLower(store.SettingString(ctx, g.Store.Settings(), settingWeeklyDay, defaultWeeklyDay)))
	if !valid || localNow.Weekday() != wd {
		return nil
	}
	// 周期起始 = 发送日 00:00（本地时区），保证同一天内幂等。
	periodStart := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), 0, 0, 0, 0, loc)
	dateKey := periodStart.Format("2006-01-02")
	if store.SettingString(ctx, g.Store.Settings(), settingLastWeekly, "") == dateKey {
		return nil
	}

	events, err := g.filteredEvents(ctx, periodStart.UTC(), weeklyMonthlyEventLimit)
	if err != nil {
		return err
	}
	if len(events) == 0 && !store.SettingBool(ctx, g.Store.Settings(), settingSendEmpty, false) {
		return nil
	}

	title := fmt.Sprintf("📊 每周报告 %s 起", dateKey)
	body, aiUsed := g.reportBody(ctx, title, events, "过去 7 天", now)
	return g.enqueue(ctx, settingLastWeekly, "report|weekly", dateKey, title, body, map[string]any{
		"report": "weekly", "period_start": dateKey, "count": len(events), "ai": aiUsed,
	})
}

// RunMonthly 每月报告：在配置的发送日到达发送时刻且当月未发送时生成。
// 周期为滚动 30 天，幂等键按月稳定。
func (g *Generator) RunMonthly(ctx context.Context, now time.Time) error {
	if !store.SettingBool(ctx, g.Store.Settings(), settingMonthlyEnabled, false) {
		return nil
	}
	loc, ok := g.sendWindow(ctx, now)
	if !ok {
		return nil
	}
	localNow := now.In(loc)
	day := store.SettingInt(ctx, g.Store.Settings(), settingMonthlyDay, defaultMonthlyDay)
	if localNow.Day() != day {
		return nil
	}
	dateKey := localNow.Format("2006-01")
	if store.SettingString(ctx, g.Store.Settings(), settingLastMonthly, "") == dateKey {
		return nil
	}

	events, err := g.filteredEvents(ctx, now.AddDate(0, 0, -30), weeklyMonthlyEventLimit)
	if err != nil {
		return err
	}
	if len(events) == 0 && !store.SettingBool(ctx, g.Store.Settings(), settingSendEmpty, false) {
		return nil
	}

	title := fmt.Sprintf("📊 每月报告 %s", dateKey)
	body, aiUsed := g.reportBody(ctx, title, events, "过去 30 天", now)
	return g.enqueue(ctx, settingLastMonthly, "report|monthly", dateKey, title, body, map[string]any{
		"report": "monthly", "period": dateKey, "count": len(events), "ai": aiUsed,
	})
}

// sendWindow 计算本地时区并判定是否处于发送时刻后的一小时窗口内。
// 返回时区与是否在窗口内。
func (g *Generator) sendWindow(ctx context.Context, now time.Time) (*time.Location, bool) {
	tzName := store.SettingString(ctx, g.Store.Settings(), settingTimezone, "UTC")
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		loc = time.UTC
	}
	localNow := now.In(loc)
	sendAt := store.SettingString(ctx, g.Store.Settings(), settingLocalTime, "09:00")
	var hour, minute int
	if _, err := fmt.Sscanf(sendAt, "%d:%d", &hour, &minute); err != nil {
		hour, minute = 9, 0
	}
	// 发送窗口 = 本地发送时刻起一小时：按时刻比较而非小时+分钟分段，
	// 避免 sendAt 分钟越大窗口越逼近两小时（此前 09:30 会开放到 10:59）。
	sendAtTime := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), hour, minute, 0, 0, loc)
	if localNow.Before(sendAtTime) || !localNow.Before(sendAtTime.Add(time.Hour)) {
		return loc, false
	}
	return loc, true
}

// filteredEvents 读取某时间窗内的事件并按全局功能开关过滤。
func (g *Generator) filteredEvents(ctx context.Context, since time.Time, limit int) ([]store.Event, error) {
	events, err := g.Store.Events().ListSince(ctx, since, limit)
	if err != nil {
		return nil, err
	}
	features := store.LoadFeatureFlags(ctx, g.Store.Settings())
	filtered := events[:0]
	for _, ev := range events {
		if features.AllowsKind(ev.Kind) {
			filtered = append(filtered, ev)
		}
	}
	return filtered, nil
}

// reportBody 生成定期报告正文：AI 可用时优先使用 AI 总结，失败回退模板。
// 返回正文与是否使用了 AI。
// 日志留痕 AI 参与度：skipped（未启用/无事件）、used（AI 总结成功）、
// fallback（AI 调用失败/输出为空/质量不达标，回退模板），配合 ai 层 ai request ok/failed 日志
// （携带同一 req_id），可完整还原「有没有发请求、AI 有没有参与、失败是网络还是上游问题」。
func (g *Generator) reportBody(ctx context.Context, title string, events []store.Event, period string, now time.Time) (string, bool) {
	// 仓库名映射一次批量拉取，模板预览与 AI 总结共用，避免两处各自查询造成 N+1。
	names := g.repoNames(ctx, events)
	template := buildReportBody(title, events, period, names, now)
	if g.AI == nil || !g.AI.IsDigestEnabled() || len(events) == 0 {
		if g.Logger != nil {
			reason := "ai_not_enabled"
			if len(events) == 0 {
				reason = "no_events"
			}
			g.Logger.Info("digest ai skipped", "title", title, "events", len(events), "reason", reason)
		}
		return template, false
	}
	// 为本次 AI 决策注入请求关联 ID：参与度日志与 ai 层调用日志共用同一 req_id。
	ctx, reqID := ai.EnsureRequestID(ctx)
	start := time.Now()
	summary, err := g.AI.SummarizeEvents(ctx, events, names, period)
	duration := time.Since(start)
	if err != nil || strings.TrimSpace(summary) == "" {
		if g.Logger != nil {
			reason := "empty_summary"
			if err != nil {
				reason = "ai_error"
			}
			attrs := []any{"req_id", reqID, "title", title, "events", len(events), "duration_ms", duration.Milliseconds(), "reason", reason}
			if err != nil {
				attrs = append(attrs, "error", err.Error())
			}
			g.Logger.Warn("digest ai fallback", attrs...)
		}
		return template, false
	}
	// 质量护栏：AI 输出过短或复读模板预览头（「最近活动：」）视为低质，
	// 回退模板避免「AI 输出反而更差」；阈值保守，宁可多回退。
	if len([]rune(summary)) < minSummaryRunes || strings.Contains(summary, "最近活动：") {
		if g.Logger != nil {
			g.Logger.Warn("digest ai fallback",
				"req_id", reqID, "title", title, "events", len(events),
				"duration_ms", duration.Milliseconds(), "reason", "low_quality")
		}
		return template, false
	}
	if g.Logger != nil {
		g.Logger.Info("digest ai used", "req_id", reqID, "title", title, "events", len(events), "duration_ms", duration.Milliseconds())
	}
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")
	// AI 输出为模型生成文本，嵌入 HTML 正文前必须转义，避免破坏 parse_mode=HTML。
	b.WriteString(htmlpkg.EscapeString(summary))
	return b.String(), true
}

// repoNames 解析事件涉及的仓库 ID → full_name 映射（供 AI 总结引用仓库名）。
// 一次 List 批量拉取（仓库超 100 时翻页），避免对每个仓库单独 Get 造成 N+1 查询。
// 查询失败或仓库不存在时跳过，不阻断总结。
func (g *Generator) repoNames(ctx context.Context, events []store.Event) map[string]string {
	need := make(map[string]struct{})
	for _, ev := range events {
		if ev.RepositoryID != nil {
			need[*ev.RepositoryID] = struct{}{}
		}
	}
	out := make(map[string]string, len(need))
	if len(need) == 0 {
		return out
	}
	for page := 1; ; page++ {
		repos, res, err := g.Store.Repositories().List(ctx, store.ListFilter{Page: page, PerPage: 100})
		if err != nil {
			return out
		}
		for _, repo := range repos {
			if _, ok := need[repo.ID]; ok {
				out[repo.ID] = repo.FullName
			}
		}
		if page*res.PerPage >= res.Total || len(repos) == 0 {
			break
		}
	}
	return out
}

// enqueue 向启用定期汇总的渠道写入 outbox，并记录 last-sent 记账键。
func (g *Generator) enqueue(
	ctx context.Context,
	lastKey, prefix, dateKey, title, body string,
	bodyJSON map[string]any,
) error {
	channels, err := g.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	enqueued := 0
	for _, ch := range channels {
		if !ch.Enabled || !ch.DigestEnabled {
			continue
		}
		idem := fmt.Sprintf("%s|%s|%s", prefix, ch.ID, dateKey)
		_, err := g.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: bodyJSON,
		})
		if err != nil && !errors.Is(err, store.ErrConflict) {
			return err
		}
		enqueued++
	}
	if enqueued == 0 && g.Logger != nil {
		// 报告生成了但无渠道接收（未启用或未勾选定期汇总）：用户看不到是常见困惑点，Debug 留痕。
		g.Logger.Debug("digest enqueued to no channel", "title", title, "ledger_key", lastKey)
	}
	// 记账与渠道无关：即使暂无可投递渠道也落账，避免之后开启订阅时当日补发。
	raw, _ := json.Marshal(dateKey)
	if _, err := g.Store.Settings().Upsert(ctx, store.SystemSetting{
		ID: ulid.Make().String(), Key: lastKey, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "system",
	}); err != nil && g.Logger != nil {
		// 记账失败会让下次调度重跑本轮生成（幂等键挡重复投递，但白跑一轮），留痕便于排查。
		g.Logger.Warn("digest ledger upsert failed", "key", lastKey, "error_code", "ledger_upsert_failed", "error", err.Error())
	}
	return nil
}

// buildReportBody 构建分组格式的定期报告正文。
// repoNames 为仓库 ID → full_name 映射（可为 nil）：预览行带仓库名便于多仓用户
// 一眼区分事件归属；映射缺失时回退原格式（不带仓库前缀）。
// generatedAt 为报告生成时刻：页脚标注生成时间（UTC，与规则通知时间格式一致），
// 让用户能判断报告新鲜度，避免把旧报告误认为当前时刻。
func buildReportBody(title string, events []store.Event, period string, repoNames map[string]string, generatedAt time.Time) string {
	var b strings.Builder
	b.WriteString(fmt.Sprintf("<b>%s</b>\n", title))
	b.WriteString("────────────────\n")

	if len(events) == 0 {
		// 空事件文案带上周期（过去 24 小时/7 天/30 天），避免三类报告共用一句含糊的「期间」。
		// 用 📭 而非庆祝 emoji：无事发生不宜用庆祝语气。
		b.WriteString(fmt.Sprintf("📭 %s无新事件\n", period))
		appendReportFooter(&b, generatedAt)
		return b.String()
	}

	b.WriteString(fmt.Sprintf("%s共 %d 条事件\n", period, len(events)))
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
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].count != sorted[j].count {
			return sorted[i].count > sorted[j].count
		}
		return sorted[i].kind < sorted[j].kind
	})
	for _, g := range sorted {
		b.WriteString(fmt.Sprintf("%s %s × %d\n", rules.KindEmoji(g.kind), store.KindDisplayName(g.kind), g.count))
	}

	// 最近 5 条事件预览（状态中文一眼可读，多仓用户靠仓库名区分归属）
	maxPreview := maxPreviewEvents
	b.WriteString("────────────────\n")
	b.WriteString("最近活动：\n")
	for i, ev := range events {
		if i >= maxPreview {
			break
		}
		status := digestStatusLabel(ev)
		repoPrefix := ""
		// release 事件（star 追踪）无 RepositoryID，经 EventRepoName 回退 PayloadSummary 补仓库名；
		// star/watch 事件标题即仓库名，不再重复前缀。
		if name := store.EventRepoName(ev, repoNames); name != "" && name != ev.Title {
			repoPrefix = name
		}
		numStr := ""
		if ev.SubjectNumber != nil {
			numStr = fmt.Sprintf("#%d", *ev.SubjectNumber)
		}
		// 标题来自 GitHub 用户输入，正文以 ParseMode=HTML 发送，必须转义，
		// 否则 <、& 等字符会破坏消息或注入 HTML（与 renderMessage 保持一致）。
		// 按段拼接避免「无仓库且无编号」时出现双空格。
		var line strings.Builder
		line.WriteString(fmt.Sprintf("• [%s]", status))
		if repoPrefix != "" {
			line.WriteString(" " + repoPrefix + numStr)
		} else if numStr != "" {
			line.WriteString(" " + numStr)
		}
		line.WriteString(" " + htmlpkg.EscapeString(ev.Title))
		b.WriteString(line.String() + "\n")
	}

	appendReportFooter(&b, generatedAt)
	return b.String()
}

// appendReportFooter 追加报告页脚：生成时间（UTC），与规则通知的「⏰ 时间」格式保持一致。
func appendReportFooter(b *strings.Builder, generatedAt time.Time) {
	b.WriteString("────────────────\n")
	b.WriteString(fmt.Sprintf("⏰ 生成时间：%s\n", generatedAt.UTC().Format("2006-01-02 15:04 UTC")))
}

// parseWeekday 将英文周名解析为 time.Weekday；非法返回 false。
func parseWeekday(s string) (time.Weekday, bool) {
	switch s {
	case "sunday":
		return time.Sunday, true
	case "monday":
		return time.Monday, true
	case "tuesday":
		return time.Tuesday, true
	case "wednesday":
		return time.Wednesday, true
	case "thursday":
		return time.Thursday, true
	case "friday":
		return time.Friday, true
	case "saturday":
		return time.Saturday, true
	default:
		return 0, false
	}
}

// digestStatusLabel 摘要预览用的短状态文案：直接复用 rules.EventStatusLabel，
// 与实时通知（engine.go）的状态语义同源，避免第二套映射漂移；
// 语义差异（报告预览不需要 emoji）由该函数收敛。
func digestStatusLabel(ev store.Event) string {
	return rules.EventStatusLabel(&ev)
}

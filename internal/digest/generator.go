package digest

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"log/slog"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
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
	// JevRuntime 可选；判别式决策引擎运行时配置与客户端。
	JevRuntime *jev.RuntimeConfig
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

	title := "📊 每日摘要 " + dateKey
	return g.enqueue(ctx, settingLastDigest, "digest", dateKey, title, "过去 24 小时", now, events, map[string]any{
		"digest": true, "date": dateKey,
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

	title := "📊 每周报告 " + dateKey + " 起"
	return g.enqueue(ctx, settingLastWeekly, "report|weekly", dateKey, title, "过去 7 天", now, events, map[string]any{
		"report": "weekly", "period_start": dateKey,
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

	title := "📊 每月报告 " + dateKey
	return g.enqueue(ctx, settingLastMonthly, "report|monthly", dateKey, title, "过去 30 天", now, events, map[string]any{
		"report": "monthly", "period": dateKey,
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
	hour, minute := 9, 0
	if hStr, mStr, ok := strings.Cut(sendAt, ":"); ok {
		if h, err := strconv.Atoi(strings.TrimSpace(hStr)); err == nil && h >= 0 && h < 24 {
			if m, err := strconv.Atoi(strings.TrimSpace(mStr)); err == nil && m >= 0 && m < 60 {
				hour, minute = h, m
			}
		}
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
// （携带同一 req_id），可完整还原「有没有发起请求、AI 有没有参与、失败是网络还是上游问题」。
func (g *Generator) reportBody(ctx context.Context, title string, events []store.Event, period string, now time.Time) (string, bool) {
	events = g.enrichEventStates(ctx, events)
	// 仓库名映射一次批量拉取，模板预览与 AI 总结共用，避免两处各自查询造成 N+1。
	names := g.repoNames(ctx, events)
	since := periodToSince(period, now)
	jevSummary := g.collectJevSummary(ctx, since, events, names)
	jevSection := buildDigestJevSection(jevSummary)
	template := buildReportBody(title, events, period, names, now, jevSection)
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
	if utf8.RuneCountInString(summary) < minSummaryRunes || strings.Contains(summary, "最近活动：") {
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
	b.Grow(len(title) + len(summary) + 512)
	b.WriteString("<b>")
	b.WriteString(htmlpkg.EscapeString(title))
	b.WriteString("</b>\n")
	b.WriteString("────────────────\n")
	// 顶部生成紧凑的数据速览看板（高危告警、缺陷、发布、依赖），0.5 秒感知全局健康度
	if card := buildDigestMetricsCard(events, period, jevSummary); card != "" {
		b.WriteString(card)
	}
	// 将 AI 总结格式化为视觉分层且支持 Telegram 原生折叠块（<blockquote expandable>）的高级富文本
	b.WriteString(formatDigestSummaryHTML(summary))
	if jevSection != "" {
		b.WriteString("\n\n")
		b.WriteString(jevSection)
	}
	return b.String(), true
}

// repoNames 解析事件涉及的仓库 ID → full_name 映射（供 AI 总结引用仓库名）。
// 一次 List 批量拉取（仓库超 100 时翻页），避免对每个仓库单独 Get 造成 N+1 查询。\n// 查询失败或仓库不存在时跳过，不阻断总结。
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
	// 少量仓库（常见场景：1~3 个仓产生活动）：直接按主键点查，避免全表分页与扫描
	if len(need) <= 3 {
		for id := range need {
			if repo, err := g.Store.Repositories().Get(ctx, id); err == nil {
				out[id] = repo.FullName
			}
		}
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
		if len(out) == len(need) || page*res.PerPage >= res.Total || len(repos) == 0 {
			break
		}
	}
	return out
}

// enqueue 向启用定期汇总的渠道写入 outbox，并记录 last-sent 记账键。
// 正文按渠道的机器人免打扰配置生成：开启 IgnoreBots 的渠道使用剔除机器人工作项后的事件集，
// 其余渠道使用完整事件集；两种变体各自只生成一次（含 AI 总结与仓库名映射），避免重复调用模型。
// 渠道过滤后无有效事件（报告内容全是机器人动态）时跳过该渠道：免打扰的渠道不应收到空报告。
func (g *Generator) enqueue(
	ctx context.Context,
	lastKey, prefix, dateKey, title, period string,
	now time.Time,
	events []store.Event,
	extraJSON map[string]any,
) error {
	channels, err := g.Store.Channels().List(ctx)
	if err != nil {
		return err
	}

	// 变体 0 = 完整事件集；变体 1 = 剔除机器人工作项后的事件集。
	// 仅当存在开启 IgnoreBots 的渠道且确实过滤掉事件时才需要变体 1，因此惰性生成。
	// 以「开启 IgnoreBots 的渠道」为参照，复用 rules 的单一判定剔除机器人工作项事件。
	filteredEvents := make([]store.Event, 0, len(events))
	botFilterChannel := store.NotificationChannel{IgnoreBots: true}
	for _, ev := range events {
		if rules.ShouldSuppressBotEvent(botFilterChannel, &ev) {
			continue
		}
		filteredEvents = append(filteredEvents, ev)
	}
	botFiltered := len(filteredEvents) != len(events)

	var (
		bodies = [2]string{}
		counts = [2]int{}
		aiUsed = [2]bool{}
		ready  = [2]bool{}
	)
	bodyFor := func(variant int) (string, int, bool) {
		if !ready[variant] {
			evs := events
			if variant == 1 {
				evs = filteredEvents
			}
			bodies[variant], aiUsed[variant] = g.reportBody(ctx, title, evs, period, now)
			counts[variant] = len(evs)
			ready[variant] = true
		}
		return bodies[variant], counts[variant], aiUsed[variant]
	}

	enqueued := 0
	for _, ch := range channels {
		if !ch.Enabled || !ch.DigestEnabled {
			continue
		}
		if prefix == "digest" && !ch.ReceiveDailyDigest {
			continue
		}
		if prefix == "report|weekly" && !ch.ReceiveWeeklyReport {
			continue
		}
		if prefix == "report|monthly" && !ch.ReceiveMonthlyReport {
			continue
		}
		variant := 0
		if ch.IgnoreBots && botFiltered {
			variant = 1
		}
		body, count, used := bodyFor(variant)
		if variant == 1 && count == 0 {
			// 报告内容全部是机器人动态：该渠道开启免打扰，跳过投递避免空报告。
			if g.Logger != nil {
				g.Logger.Debug("digest skipped for bot-free channel", "title", title, "channel_id", ch.ID)
			}
			continue
		}
		payload := make(map[string]any, len(extraJSON)+2)
		for k, v := range extraJSON {
			payload[k] = v
		}
		payload["count"] = count
		payload["ai"] = used
		idem := prefix + "|" + ch.ID + "|" + dateKey
		_, err := g.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: time.Now().UTC(),
			Title: title, BodyText: body, ParseMode: "HTML",
			BodyJSON: payload,
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
// extraSections 支持附加额外结构化信息（如 Jev 运行概况），附于活动预览之后、页脚之前。
func buildReportBody(title string, events []store.Event, period string, repoNames map[string]string, generatedAt time.Time, extraSections ...string) string {
	var b strings.Builder
	b.Grow(1024)
	b.WriteString("<b>")
	b.WriteString(htmlpkg.EscapeString(title))
	b.WriteString("</b>\n────────────────\n")

	if len(events) == 0 {
		// 空事件文案带上周期（过去 24 小时/7 天/30 天），避免三类报告共用一句含糊的「期间」。
		b.WriteString("✨ <b>系统运行平稳 · 监控哨兵持续值守</b>\n")
		b.WriteString("📭 ")
		b.WriteString(period)
		b.WriteString("无新事件\n")
		b.WriteString("<blockquote expandable>\n<b>🛡️ 值守状态说明</b>\n受监控仓库未发生新的安全告警、CI 异常中断或阻塞性问题，代码资产运行处于健康状态。\n</blockquote>\n")
		for _, sec := range extraSections {
			if strings.TrimSpace(sec) != "" {
				b.WriteString("────────────────\n")
				b.WriteString(sec)
				b.WriteString("\n")
			}
		}
		appendReportFooter(&b, generatedAt)
		return b.String()
	}

	b.WriteString(period)
	b.WriteString("共 ")
	b.WriteString(strconv.Itoa(len(events)))
	b.WriteString(" 条事件\n────────────────\n")

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
		b.WriteString(rules.KindEmoji(g.kind))
		b.WriteString(" ")
		b.WriteString(store.KindDisplayName(g.kind))
		b.WriteString(" × ")
		b.WriteString(strconv.Itoa(g.count))
		b.WriteString("\n")
	}

	// 多仓监控时呈现焦点活跃仓库排行
	repoCounts := make(map[string]int)
	for _, ev := range events {
		if name := store.EventRepoName(ev, repoNames); name != "" && name != ev.Title {
			repoCounts[name]++
		}
	}
	if len(repoCounts) > 1 {
		type rkv struct {
			name  string
			count int
		}
		var rsorted []rkv
		for k, v := range repoCounts {
			rsorted = append(rsorted, rkv{k, v})
		}
		sort.Slice(rsorted, func(i, j int) bool {
			if rsorted[i].count != rsorted[j].count {
				return rsorted[i].count > rsorted[j].count
			}
			return rsorted[i].name < rsorted[j].name
		})
		b.WriteString("────────────────\n🔥 活跃仓库：")
		var topList []string
		for idx, r := range rsorted {
			if idx >= 3 {
				break
			}
			topList = append(topList, fmt.Sprintf("%s (%d)", r.name, r.count))
		}
		b.WriteString(strings.Join(topList, " · "))
		b.WriteString("\n")
	}

	// 最近活动预览（状态中文一眼可读，多仓用户靠仓库名区分归属；条数见 maxPreviewEvents）
	b.WriteString("────────────────\n最近活动：\n")
	for i, ev := range events {
		if i >= maxPreviewEvents {
			break
		}
		status := digestStatusLabel(ev)
		repoPrefix := ""
		// release 事件（star 追踪）无 RepositoryID，经 EventRepoName 回退 PayloadSummary 补仓库名；
		// star/watch 事件标题即仓库名，不再重复前缀。
		if name := store.EventRepoName(ev, repoNames); name != "" && name != ev.Title {
			repoPrefix = htmlpkg.EscapeString(name)
		}
		numStr := ""
		if ev.SubjectNumber != nil {
			numStr = "#" + strconv.FormatInt(*ev.SubjectNumber, 10)
		}
		// 标题来自 GitHub 用户输入，正文以 ParseMode=HTML 发送，必须转义，
		// 否则 <、& 等字符会破坏消息或注入 HTML（与 renderMessage 保持一致）。
		// 按段拼接避免「无仓库且无编号」时出现双空格。
		var line strings.Builder
		line.Grow(128)
		line.WriteString("• [")
		line.WriteString(status)
		line.WriteString("]")
		if repoPrefix != "" {
			line.WriteString(" ")
			line.WriteString(repoPrefix)
			line.WriteString(numStr)
		} else if numStr != "" {
			line.WriteString(" ")
			line.WriteString(numStr)
		}
		line.WriteString(" ")
		line.WriteString(htmlpkg.EscapeString(ev.Title))
		b.WriteString(line.String())
		b.WriteString("\n")
	}

	for _, sec := range extraSections {
		if strings.TrimSpace(sec) != "" {
			b.WriteString("────────────────\n")
			b.WriteString(sec)
			b.WriteString("\n")
		}
	}

	appendReportFooter(&b, generatedAt)
	return b.String()
}

func appendReportFooter(b *strings.Builder, generatedAt time.Time) {
	if generatedAt.IsZero() {
		return
	}
	b.WriteString("────────────────\n<i>⏰ 生成时间：")
	b.WriteString(generatedAt.UTC().Format("2006-01-02 15:04 UTC"))
	b.WriteString("</i>\n")
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

// enrichEventStates 检查事件关联实体在 store 中的最新状态，若数据库中已为已修复/已关闭/已恢复终态，则补充至事件元数据中。
func (g *Generator) enrichEventStates(ctx context.Context, events []store.Event) []store.Event {
	if len(events) == 0 || g.Store == nil {
		return events
	}
	out := make([]store.Event, len(events))
	copy(out, events)

	type entityKey struct {
		repoID string
		kind   string
		number int64
	}

	// 1. 本地批次内部状态收敛：若同一实体在时间段内存在已关闭/已修复事件，所有更早记录也标记为已修复
	resolvedInBatch := make(map[entityKey]string)
	for _, ev := range out {
		if ev.RepositoryID == nil || ev.SubjectNumber == nil {
			continue
		}
		key := entityKey{repoID: *ev.RepositoryID, kind: ev.Kind, number: *ev.SubjectNumber}
		if isEventClosedOrFixed(ev) {
			if _, exists := resolvedInBatch[key]; !exists {
				act := ev.Action
				if act == "" {
					act = "fixed"
				}
				resolvedInBatch[key] = act
			}
		}
	}

	// 2. 针对仍未在 batch 内发现 closed 的告警与工作项，查询数据库最新状态
	alertCache := make(map[entityKey]string)
	issueCache := make(map[entityKey]string)

	for i := range out {
		ev := &out[i]
		if ev.RepositoryID == nil || ev.SubjectNumber == nil {
			continue
		}
		key := entityKey{repoID: *ev.RepositoryID, kind: ev.Kind, number: *ev.SubjectNumber}

		if act, ok := resolvedInBatch[key]; ok && !isEventClosedOrFixed(*ev) {
			ev.Action = act
			if ev.PayloadSummary == nil {
				ev.PayloadSummary = make(map[string]any)
			}
			ev.PayloadSummary["state"] = act
			continue
		}

		if isEventClosedOrFixed(*ev) {
			continue
		}

		switch ev.Kind {
		case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
			st, ok := alertCache[key]
			if !ok {
				if sa, err := g.Store.SecurityAlerts().GetByIdentity(ctx, key.repoID, key.kind, int(key.number)); err == nil {
					st = sa.State
				}
				alertCache[key] = st
			}
			switch strings.ToLower(st) {
			case "fixed", "resolved", "dismissed", "auto_dismissed", "withdrawn":
				ev.Action = st
				if ev.PayloadSummary == nil {
					ev.PayloadSummary = make(map[string]any)
				}
				ev.PayloadSummary["state"] = st
			}

		case store.WorkItemKindIssue, store.WorkItemKindPR:
			st, ok := issueCache[key]
			if !ok {
				if wi, err := g.Store.WorkItems().GetByRepoNumber(ctx, key.repoID, int(key.number)); err == nil {
					if wi.Merged {
						st = "merged"
					} else {
						st = wi.State
					}
				}
				issueCache[key] = st
			}
			if strings.ToLower(st) == "closed" || strings.ToLower(st) == "merged" {
				ev.Action = st
				if ev.PayloadSummary == nil {
					ev.PayloadSummary = make(map[string]any)
				}
				ev.PayloadSummary["state"] = st
			}
		}
	}
	return out
}

func periodToSince(period string, now time.Time) time.Time {
	switch period {
	case "过去 24 小时":
		return now.Add(-24 * time.Hour)
	case "过去 7 天":
		return now.AddDate(0, 0, -7)
	case "过去 30 天":
		return now.AddDate(0, 0, -30)
	default:
		return now.Add(-24 * time.Hour)
	}
}

func (g *Generator) resolveJevConfig(ctx context.Context) (enabled bool, mode, model string, circuitState string) {
	if g.JevRuntime != nil {
		snap := g.JevRuntime.Snapshot()
		circuit := "closed"
		for _, bs := range g.JevRuntime.BreakerStats() {
			if bs.State == "open" {
				circuit = "open"
				break
			} else if bs.State == "half_open" && circuit != "open" {
				circuit = "half_open"
			}
		}
		return snap.Enabled, snap.Mode, snap.Model, circuit
	}
	if g.Store != nil {
		cfg, err := jev.LoadStoredConfig(ctx, g.Store)
		if err == nil && cfg.Enabled != nil && *cfg.Enabled {
			mode := cfg.Mode
			if mode == "" {
				mode = "shadow"
			}
			return true, mode, cfg.Model, "closed"
		}
	}
	return false, "", "", "closed"
}

func (g *Generator) collectJevSummary(ctx context.Context, since time.Time, events []store.Event, names map[string]string) *JevExecutionSummary {
	enabled, mode, model, circuitState := g.resolveJevConfig(ctx)
	if !enabled {
		return nil
	}

	mSnap := jev.GlobalMetrics().Snapshot()
	var totalCalls, totalSuccesses, totalHits, totalMisses uint64
	var maxP50, maxP95 float64
	for _, c := range mSnap.Calls {
		totalCalls += c
	}
	for _, s := range mSnap.Successes {
		totalSuccesses += s
	}
	for _, h := range mSnap.CacheHits {
		totalHits += h
	}
	for _, m := range mSnap.CacheMisses {
		totalMisses += m
	}
	for _, p := range mSnap.LatencyP50 {
		if p > maxP50 {
			maxP50 = p
		}
	}
	for _, p := range mSnap.LatencyP95 {
		if p > maxP95 {
			maxP95 = p
		}
	}

	successRate := 1.0
	if totalCalls > 0 {
		successRate = float64(totalSuccesses) / float64(totalCalls)
	}
	cacheHitRate := 0.0
	if totalHits+totalMisses > 0 {
		cacheHitRate = float64(totalHits) / float64(totalHits+totalMisses) * 100
	}

	sum := &JevExecutionSummary{
		Enabled:            true,
		Mode:               mode,
		Model:              model,
		CircuitState:       circuitState,
		TotalRequests:      totalCalls,
		SuccessRate:        successRate,
		LatencyP50Ms:       maxP50,
		LatencyP95Ms:       maxP95,
		CacheHitRate:       cacheHitRate,
		TotalSuppressed:    mSnap.Suppressed,
		TotalSkipped:       mSnap.Skipped,
		TotalWouldSuppress: mSnap.WouldSuppress,
		TotalWouldSkip:     mSnap.WouldSkip,
	}

	// 1. 查询时段内通知降噪拦截记录 (Audits)
	if g.Store != nil {
		var curTime time.Time
		var curID string
		for {
			logs, err := g.Store.Audits().ListCursor(ctx, curTime, curID, 50)
			if err != nil || len(logs) == 0 {
				break
			}
			reachedEnd := false
			for _, log := range logs {
				if log.CreatedAt.Before(since) {
					reachedEnd = true
					break
				}
				if log.Action == "jev.notification.suppressed" {
					sum.PeriodSuppressed++
					if len(sum.SuppressedItems) < 5 {
						var meta struct {
							Repo  string `json:"repo"`
							Title string `json:"title"`
						}
						_ = json.Unmarshal(log.MetadataJSON, &meta)
						itemDesc := meta.Repo
						if meta.Title != "" {
							if itemDesc != "" {
								itemDesc += ": " + meta.Title
							} else {
								itemDesc = meta.Title
							}
						} else {
							for _, ev := range events {
								if ev.ID == log.TargetID && ev.Title != "" {
									if itemDesc != "" {
										itemDesc += ": " + ev.Title
									} else {
										itemDesc = ev.Title
									}
									break
								}
							}
						}
						if itemDesc == "" {
							itemDesc = log.TargetID
						}
						sum.SuppressedItems = append(sum.SuppressedItems, itemDesc)
					}
				}
			}
			if reachedEnd || len(logs) < 50 {
				break
			}
			curTime = logs[len(logs)-1].CreatedAt
			curID = logs[len(logs)-1].ID
		}
	}

	// 2. 查询时段内 PR 审查门控跳过记录 (WorkItems & Settings)
	if g.Store != nil && len(events) > 0 {
		seenPRs := make(map[string]bool)
		for _, ev := range events {
			if ev.Kind != store.WorkItemKindPR || ev.RepositoryID == nil || ev.SubjectNumber == nil {
				continue
			}
			prKey := fmt.Sprintf("%s#%d", *ev.RepositoryID, *ev.SubjectNumber)
			if seenPRs[prKey] {
				continue
			}
			seenPRs[prKey] = true

			wi, err := g.Store.WorkItems().GetByRepoNumber(ctx, *ev.RepositoryID, int(*ev.SubjectNumber))
			if err != nil || wi.ID == "" {
				continue
			}
			setting, err := g.Store.Settings().Get(ctx, "ai.pr_review."+wi.ID)
			if err != nil {
				continue
			}
			var review ai.CodeReviewResult
			if err := json.Unmarshal(setting.ValueJSON, &review); err != nil {
				continue
			}
			if review.ReviewStatus == "skipped" && (review.DecisionModel == "jev" || review.SkipReason == "jev_documentation_only") {
				sum.PeriodSkippedPRs++
				if len(sum.SkippedPRItems) < 5 {
					repoName := names[*ev.RepositoryID]
					if repoName == "" {
						repoName = *ev.RepositoryID
					}
					itemDesc := fmt.Sprintf("%s #%d", repoName, *ev.SubjectNumber)
					if ev.Title != "" {
						itemDesc += ": " + ev.Title
					}
					sum.SkippedPRItems = append(sum.SkippedPRItems, itemDesc)
				}
			}
		}
	}

	return sum
}

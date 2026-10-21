package rules

import (
	"context"
	"encoding/json"
	"errors"
	htmlpkg "html"
	"log/slog"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// Aggregator 同仓同类事件短时合并，抑制通知风暴。
type Aggregator struct {
	Store          store.Store
	Window         time.Duration
	BurstThreshold int
	BurstWindow    time.Duration
	// AI 可选；flush 单事件回放时透传给 Engine 供安全告警分诊。
	AI *ai.Client
	// Logger 可选；透传给 Engine 供分诊参与度留痕。
	Logger *slog.Logger
	// GitHub 可选；透传给 Engine 供拉取 CI 失败 Job/步骤。
	GitHub *githubx.AppClient
	// JevClient 可选；判别式决策引擎客户端。
	JevClient jev.Client
	// JevConfig 判别式决策引擎配置。
	JevConfig config.JevConfig
	// JevRuntime 可选；动态运行时配置与客户端提供者。
	JevRuntime *jev.RuntimeConfig

	mu sync.Mutex
	// key: repoID|category
	buckets map[string]*aggBucket
	// burst: repoID|category -> 滑动窗口内的事件时间戳
	bursts map[string][]time.Time
	// burstNotified: repoID|category -> 最近一次已投递超频摘要的时间桶。
	// 同桶内后续超频事件只计入窗口计数，不再重复写库与留痕。
	burstNotified map[string]int64
	// flushedVariants: 「窗口幂等变体」-> 该变体最近一次入队的时间桶。
	// 用于区分「本进程同窗口的第二批事件」（必须补发，否则撞唯一约束丢失）
	// 与「其他副本已入队」（保持原键，维持跨实例收敛）。
	flushedVariants map[string]int64
}

func (a *Aggregator) resolveJev() (jev.Client, config.JevConfig) {
	if a != nil && a.JevRuntime != nil {
		return a.JevRuntime.Client("notification"), a.JevRuntime.Snapshot()
	}
	if a != nil {
		return a.JevClient, a.JevConfig
	}
	return nil, config.JevConfig{}
}

type aggBucket struct {
	category string
	repoID   string
	repoName string
	events   []*store.Event
	prepared []preparedEvent
	timer    *time.Timer
}

func (b *aggBucket) preparedItems() []preparedEvent {
	if b == nil {
		return nil
	}
	if len(b.prepared) > 0 {
		return b.prepared
	}
	var res []preparedEvent
	for _, ev := range b.events {
		if ev != nil {
			res = append(res, preparedEvent{
				Result:        normalizer.Result{Event: ev},
				InferredEvent: ev,
				Channels:      nil,
			})
		}
	}
	return res
}

// NewAggregator 创建聚合器。
func NewAggregator(st store.Store, window time.Duration, burstThreshold int, burstWindow time.Duration) *Aggregator {
	if window <= 0 {
		window = 60 * time.Second
	}
	if burstThreshold <= 0 {
		burstThreshold = 15
	}
	if burstWindow <= 0 {
		burstWindow = 5 * time.Minute
	}
	return &Aggregator{
		Store: st, Window: window, BurstThreshold: burstThreshold, BurstWindow: burstWindow,
		buckets: make(map[string]*aggBucket), bursts: make(map[string][]time.Time),
		burstNotified:   make(map[string]int64),
		flushedVariants: make(map[string]int64),
	}
}

// maxTrackedFlushedVariants 内存登记表上限：超过后按时间桶清理旧条目，避免长期运行无界增长。
const maxTrackedFlushedVariants = 4096

// markVariantFlushed 登记一次「窗口幂等变体」入队；返回该变体在本进程是否已被入队过。
// 同一窗口内第二次及以后的入队需要带批次指纹区分，否则会撞 Outbox 唯一约束被静默丢弃。
func (a *Aggregator) markVariantFlushed(variant string, bucket int64) (alreadyFlushed bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.flushedVariants == nil {
		a.flushedVariants = make(map[string]int64)
	}
	if _, ok := a.flushedVariants[variant]; ok {
		a.flushedVariants[variant] = bucket
		return true
	}
	if len(a.flushedVariants) >= maxTrackedFlushedVariants {
		for k, v := range a.flushedVariants {
			if v < bucket {
				delete(a.flushedVariants, k)
			}
		}
		if len(a.flushedVariants) >= maxTrackedFlushedVariants {
			// 同一时间桶内变体数异常膨胀：整体清空，最坏结果是同窗口多一条合并通知，
			// 也不会让登记表无界增长。
			a.flushedVariants = make(map[string]int64, maxTrackedFlushedVariants)
		}
	}
	a.flushedVariants[variant] = bucket
	return false
}

// FlushAll 立即清空并提交所有暂存中的聚合桶（如优雅停机或即时刷新）。
func (a *Aggregator) FlushAll() {
	if a == nil {
		return
	}
	a.mu.Lock()
	keys := make([]string, 0, len(a.buckets))
	for k, b := range a.buckets {
		if b.timer != nil {
			b.timer.Stop()
		}
		keys = append(keys, k)
	}
	a.mu.Unlock()
	for _, k := range keys {
		a.flush(k, nil)
	}
}

// ReloadFrom 从 system_settings 热加载聚合参数；未设置或非法的键保留当前值。
// 管理台修改 notify.* 后调用，参数即时生效而无需重启。
func (a *Aggregator) ReloadFrom(ctx context.Context) error {
	if a == nil || a.Store == nil {
		return nil
	}
	window, err1 := readPositiveIntSetting(ctx, a.Store, "notify.aggregate_window_sec")
	threshold, err2 := readPositiveIntSetting(ctx, a.Store, "notify.burst_threshold")
	burstWindow, err3 := readPositiveIntSetting(ctx, a.Store, "notify.burst_window_sec")
	if err := errors.Join(err1, err2, err3); err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	if window > 0 {
		a.Window = time.Duration(window) * time.Second
	}
	if threshold > 0 {
		a.BurstThreshold = threshold
	}
	if burstWindow > 0 {
		a.BurstWindow = time.Duration(burstWindow) * time.Second
	}
	return nil
}

// readPositiveIntSetting 读取整型设置；键不存在、JSON 类型错误或数值非法时返回 0（不算错误）。
// 仅真实 DB 错误上抛：三个键都未设置是常态（默认实例），此前把 ErrNotFound 透传导致
// 设置页保存任意设置都打「aggregator reload failed」假 Warn。
func readPositiveIntSetting(ctx context.Context, st store.Store, key string) (int, error) {
	if st == nil {
		return 0, nil
	}
	row, err := st.Settings().Get(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return 0, nil
		}
		return 0, err
	}
	var v float64
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return 0, nil
	}
	n, ok := store.CoerceInt(v)
	if !ok || n <= 0 {
		return 0, nil
	}
	return n, nil
}

func categoryOf(ev *store.Event) string {
	switch ev.Kind {
	case store.AlertKindDependabot, store.AlertKindCodeScanning, store.AlertKindSecretScanning:
		return "security"
	case store.WorkflowRunKind:
		return "actions"
	case store.WorkItemKindIssue:
		return "issue"
	case store.WorkItemKindPR:
		return "pr"
	default:
		return "other"
	}
}

// Evaluate 带聚合的通知决策。
func (a *Aggregator) Evaluate(ctx context.Context, res normalizer.Result, repoFullName string) error {
	if a == nil || a.Store == nil {
		return errors.New("rules: aggregator store is required")
	}
	if res.Event == nil || res.SuppressNotify || res.Event.SuppressNotification {
		return nil
	}
	// 能力开关兜底：全局 + 仓库级；聚合窗口期间开关变化也不能漏拦。
	if !allowsEventKind(ctx, a.Store, res.Repository, res.Event.Kind) {
		return nil
	}
	if !shouldNotifyRealtime(res.Event) {
		return nil
	}

	// 锁外统一准备通知：静态预检、Jev 裁决、active 抑制落库与路由
	jevClient, jevCfg := a.resolveJev()
	prep, err := PrepareNotification(ctx, res, repoFullName, jevClient, jevCfg, a.Store)
	if err != nil {
		return err
	}
	// Suppress 则不计 burst、不入桶
	if prep.Suppressed {
		if a.Logger != nil {
			a.Logger.Debug("notification suppressed by jev in aggregator",
				"event_id", res.Event.ID, "kind", res.Event.Kind, "repo", repoFullName)
		}
		return nil
	}

	ev := prep.InferredEvent
	if ev == nil {
		ev = res.Event
	}

	repoID := ""
	if ev.RepositoryID != nil {
		repoID = *ev.RepositoryID
	}
	cat := categoryOf(ev)
	key := repoID + "|" + cat

	a.mu.Lock()
	now := time.Now()
	times := a.bursts[key]
	filtered := times[:0]
	for _, t := range times {
		if now.Sub(t) <= a.BurstWindow {
			filtered = append(filtered, t)
		}
	}
	filtered = append(filtered, now)
	a.bursts[key] = filtered
	if len(a.bursts) > 100 {
		for k, ts := range a.bursts {
			if len(ts) == 0 || now.Sub(ts[len(ts)-1]) > a.BurstWindow {
				delete(a.bursts, k)
				delete(a.burstNotified, k)
			}
		}
	}
	if len(filtered) > a.BurstThreshold {
		bucket := timeBucket(now.UTC(), a.BurstWindow)
	if a.burstNotified[key] == bucket {
			a.mu.Unlock()
			return nil
		}
		a.burstNotified[key] = bucket
		sample := ev
		targetChannels := prep.Channels
		title := "⚠️ 通知频率超限"
		if repoFullName != "" {
			title = "⚠️ 通知频率超限：" + repoFullName
		}
		eventsInWindow := len(filtered)
		a.mu.Unlock()
		enqueued, skipReason, err := a.enqueueBurstSummary(ctx, repoID, repoFullName, cat, title, sample, targetChannels...)
		if err != nil {
			return err
		}
		if a.Logger != nil {
			if enqueued {
				a.Logger.Warn("burst summary enqueued",
					"repo", repoFullName, "category", cat, "events_in_window", eventsInWindow)
			} else {
				a.Logger.Warn("burst summary skipped",
					"repo", repoFullName, "category", cat, "events_in_window", eventsInWindow,
					"reason", skipReason)
			}
		}
		return nil
	}

	b, ok := a.buckets[key]
	if !ok {
		b = &aggBucket{
			category: cat,
			repoID:   repoID,
			repoName: repoFullName,
			events:   []*store.Event{ev},
			prepared: []preparedEvent{prep},
		}
		a.buckets[key] = b
		bucketRef := b
		b.timer = time.AfterFunc(a.Window, func() {
			a.flush(key, bucketRef)
		})
		a.mu.Unlock()
		return nil
	}
	b.events = append(b.events, ev)
	b.prepared = append(b.prepared, prep)
	const maxBucketBatch = 15
	shouldEarlyFlush := len(b.prepared) >= maxBucketBatch
	if shouldEarlyFlush && b.timer != nil {
		b.timer.Stop()
	}
	bucketRef := b
	a.mu.Unlock()
	if shouldEarlyFlush {
		go a.flush(key, bucketRef)
	}
	return nil
}

// flushBudget 聚合 flush 单事件回放预算：下限 30s；AI 配置超时更高时按「超时+余量」放宽，
// 与 webhook 直发路径同一语义（避免同一事件「直发有 AI 段落、聚合回放被 30s 硬顶截断」）。
func (a *Aggregator) flushBudget() time.Duration {
	const (
		minBudget = 30 * time.Second
		margin    = 10 * time.Second
	)
	if a.AI != nil {
		if budget := a.AI.EffectiveTimeout() + margin; budget > minBudget {
			return budget
		}
	}
	return minBudget
}

func (a *Aggregator) flush(key string, expect *aggBucket) {
	a.mu.Lock()
	b, ok := a.buckets[key]
	if !ok || (expect != nil && b != expect) {
		a.mu.Unlock()
		return
	}
	delete(a.buckets, key)
	a.mu.Unlock()

	items := b.preparedItems()
	ctx, cancel := context.WithTimeout(context.Background(), a.flushBudget())
	defer cancel()
	if len(items) == 1 {
		eng := &Engine{
			Store:      a.Store,
			AI:         a.AI,
			Logger:     a.Logger,
			GitHub:     a.GitHub,
			JevClient:  a.JevClient,
			JevConfig:  a.JevConfig,
			JevRuntime: a.JevRuntime,
		}
		if err := eng.evaluatePrepared(ctx, items[0], b.repoName); err != nil && a.Logger != nil {
			a.Logger.Warn("aggregate flush evaluate failed",
				"repo", b.repoName, "category", b.category, "events", len(items), "error_code", "aggregate_flush_failed", "error", err.Error())
		}
		return
	}
	if a.Logger != nil {
		a.Logger.Debug("aggregate flushed",
			"repo", b.repoName, "category", b.category, "events", len(items))
	}
	if err := a.enqueueMerged(ctx, b); err != nil && a.Logger != nil {
		a.Logger.Warn("aggregate flush enqueue failed",
			"repo", b.repoName, "category", b.category, "events", len(items), "error_code", "aggregate_flush_failed", "error", err.Error())
	}
}

// enqueueMerged 按渠道订阅过滤桶内事件子集，逐渠道构建合并消息。
func (a *Aggregator) enqueueMerged(ctx context.Context, b *aggBucket) error {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return err
	}
	features := store.LoadFeatureFlags(ctx, a.Store.Settings())
	bucket := timeBucket(time.Now().UTC(), a.Window)
	idScope := b.repoID
	if idScope == "" {
		idScope = b.repoName
	}
	items := b.preparedItems()
	sampleEvents := make([]*store.Event, 0, len(items))
	for _, p := range items {
		ev := p.InferredEvent
		if ev == nil {
			ev = p.Result.Event
		}
		if ev != nil {
			sampleEvents = append(sampleEvents, ev)
		}
	}
	scopeVariant := "agg|" + idScope + "|" + b.category + "|" + strconv.FormatInt(bucket, 10)
	variant := scopeVariant
	if a.markVariantFlushed(scopeVariant, bucket) {
		variant = scopeVariant + "|" + shortEventTag(sampleEvents)
	}
	for _, ch := range channels {
		if !ch.Enabled {
			continue
		}
		sub := make([]*store.Event, 0, len(items))
		for _, p := range items {
			ev := p.InferredEvent
			if ev == nil {
			ev = p.Result.Event
			}
			if ev == nil {
				continue
			}
			if !features.AllowsKind(ev.Kind) || ShouldSuppressBotEvent(ch, ev) {
				continue
			}
			if p.Channels != nil {
				matched := false
				for _, pch := range p.Channels {
					if pch.ID == ch.ID {
						matched = true
						break
					}
				}
				if !matched {
					continue
				}
			} else {
				if !ch.AcceptsKind(ev.Kind) {
					continue
				}
			}
			sub = append(sub, ev)
		}
		if len(sub) == 0 {
			continue
		}
		title, body := renderMergedMessage(b.repoName, b.category, sub, time.Now().UTC())
		idem := idempotencyKey(ch.ID, idScope, variant)
		eventID := sub[0].ID
		nextAttempt := time.Now().UTC()
		action, resumeAt := DecideQuietHoursForEvents(ch, sub, nextAttempt)
		channelBody := body
		if action == ActionDeferQuietHours {
			nextAttempt = resumeAt
			channelBody = channelBody + "\n<i>🌙 免打扰时段暂存，送达时刻 " + resumeAt.Format("15:04") + "</i>"
		}
		var aggURL string
		if b.repoName != "" {
			aggURL = "https://github.com/" + b.repoName
			switch b.category {
			case "pull_request", "pr":
				aggURL += "/pulls"
			case "issue":
				aggURL += "/issues"
			case "workflow", "actions":
				aggURL += "/actions"
			case "release":
				aggURL += "/releases"
			case "security":
				aggURL += "/security"
			}
		}
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eventID,
			AggregateKey: idScope + "|" + b.category, IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: nextAttempt,
			Title: title, BodyText: channelBody, ParseMode: "HTML",
			HTMLURL:  aggURL,
			BodyJSON: map[string]any{"aggregate": true, "count": len(sub), "category": b.category, "bucket": bucket},
		})
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				if a.Logger != nil {
					a.Logger.Warn("aggregate enqueue conflict",
						"repo", b.repoName, "category", b.category, "channel_id", ch.ID,
						"events", len(sub), "reason", "idempotency_conflict")
				}
				continue
			}
			return err
		}
	}
	return nil
}

// shortEventTag 取批次首事件的稳定短标识（去重指纹优先，回退事件 ID），
// 用于区分同一时间桶内的不同批次。长值取尾部 12 个字符：ULID 的随机位在尾部，
// 取前缀会让同一毫秒内生成的事件落入同一个批次标识。
func shortEventTag(events []*store.Event) string {
	if len(events) == 0 || events[0] == nil {
		return "empty"
	}
	tag := events[0].DedupeFingerprint
	if tag == "" {
		tag = events[0].ID
	}
	if len(tag) > 12 {
		tag = tag[len(tag)-12:]
	}
	if tag == "" {
		return "empty"
	}
	return tag
}

// renderMergedMessage 由事件子集构建合并标题与正文（对 Telegram HTML 做转义）。
// windowEnd 为合并窗口结束时刻，正文标注批次时间便于用户判断通知对应的窗口。
func renderMergedMessage(repoName, category string, events []*store.Event, windowEnd time.Time) (string, string) {
	categoryCN := categoryDisplayName(category)
	// 「已聚合」而非「已合并」：避免与 PR 的「已合并」状态语义混淆
	//（同一条通知里既可能包含已合并的 PR，也可能表示本通知是聚合产物）。
	var statusSuffix string
	if len(events) > 0 {
		allSame := true
		firstEmoji, firstLabel := statusDisplay(events[0])
		for _, ev := range events[1:] {
			_, label := statusDisplay(ev)
			if label != firstLabel {
				allSame = false
				break
			}
		}
		if allSame {
			statusSuffix = " " + firstEmoji + " [" + firstLabel + "]"
		}
	}
	var botBadge string
	if len(events) > 0 {
		allRenovate := true
		allDependabot := true
		for _, ev := range events {
			tLower := strings.ToLower(ev.Title)
			aLower := strings.ToLower(ev.Actor)
			if !strings.Contains(tLower, "renovate") && !strings.Contains(aLower, "renovate") {
				allRenovate = false
			}
			if !strings.Contains(tLower, "dependabot") && !strings.Contains(aLower, "dependabot") {
				allDependabot = false
			}
		}
		if allRenovate {
			botBadge = " · 📦 Renovate"
		} else if allDependabot {
			botBadge = " · 🤖 Dependabot"
		}
	}
	title := "📋 " + repoName + "：" + categoryCN + botBadge + statusSuffix + " × " + strconv.Itoa(len(events)) + "（已聚合）"
	var body strings.Builder
	body.WriteString("<b>" + htmlpkg.EscapeString(title) + "</b>\n")
	body.WriteString("────────────────\n")
	maxSamples := 8
	for i, ev := range events {
		if i >= maxSamples {
			body.WriteString("…另有 " + strconv.Itoa(len(events)-maxSamples) + " 条\n")
			break
		}
		statusEmoji, statusLabel := statusDisplay(ev)
		numStr := ""
		if ev.SubjectNumber != nil {
			rawNum := "#" + strconv.FormatInt(*ev.SubjectNumber, 10)
			if ev.HTMLURL != "" {
				numStr = `<a href="` + htmlpkg.EscapeString(ev.HTMLURL) + `">` + rawNum + `</a> `
			} else {
				numStr = `<code>` + rawNum + `</code> `
			}
		}
		// 状态中文加粗置前，编号加超链接或等宽，富文本排版更清晰易读。
		body.WriteString(statusEmoji + " <b>[" + htmlpkg.EscapeString(statusLabel) + "]</b> " + numStr + htmlpkg.EscapeString(ev.Title) + "\n")
	}
	if !windowEnd.IsZero() {
		body.WriteString("────────────────\n")
		body.WriteString("<i>⏰ 时间：" + windowEnd.UTC().Format("2006-01-02 15:04 UTC") + "</i>\n")
	}
	return title, body.String()
}

// enqueueBurstSummary 写入超频摘要；返回是否真正落库，以及未落库的原因
// （`no_channel` 无接收渠道，`duplicate` 同渠道同时间桶已被幂等收敛）。
// 调用方据此区分「已入队」与「未入队」，避免日志虚报入队次数。
func (a *Aggregator) enqueueBurstSummary(
	ctx context.Context,
	repoID, repoName, cat, title string,
	sample *store.Event,
	targetChannels ...store.NotificationChannel,
) (bool, string, error) {
	channels, err := a.Store.Channels().List(ctx)
	if err != nil {
		return false, "", err
	}
	activeMap := make(map[string]store.NotificationChannel, len(channels))
	for _, ch := range channels {
		if ch.Enabled {
			activeMap[ch.ID] = ch
		}
	}
	var eligibleChannels []store.NotificationChannel
	if len(targetChannels) > 0 {
		for _, tch := range targetChannels {
			if ch, ok := activeMap[tch.ID]; ok {
				eligibleChannels = append(eligibleChannels, ch)
			}
		}
	} else {
		for _, ch := range channels {
			if ch.Enabled && ch.AcceptsKind(sample.Kind) && !ShouldSuppressBotEvent(ch, sample) {
				eligibleChannels = append(eligibleChannels, ch)
			}
		}
	}

	bucket := timeBucket(time.Now().UTC(), a.BurstWindow)
	categoryCN := categoryDisplayName(cat)
	now := time.Now().UTC()
	safeTitle := htmlpkg.EscapeString(title)
	safeRepo := htmlpkg.EscapeString(repoName)
	safeCat := htmlpkg.EscapeString(categoryCN)
	body := "<b>" + safeTitle + "</b>\n────────────────\n" +
		"📦 仓库：<code>" + safeRepo + "</code>\n" +
		"📋 类型：" + safeCat + "\n" +
		"⚠️ <b>已触发短时事件风暴保护（限流收敛）</b>\n" +
		"<blockquote expandable>\n" +
		"<b>🛡️ 防风暴熔断机制已生效</b>\n" +
		"为避免通知通道被突发高频事件刷屏，当前窗口内的同类事件已自动降级，完整动态请登录仪表盘查看。\n" +
		"</blockquote>\n" +
		"<i>⏰ 时间：" + now.Format("2006-01-02 15:04 UTC") + "</i>"
	burstURL := sample.HTMLURL
	if burstURL == "" && repoName != "" {
		burstURL = "https://github.com/" + repoName
	}
	enqueued := false
	duplicate := false
	for _, ch := range eligibleChannels {
		idScope := repoID
		if idScope == "" {
			idScope = repoName
		}
		variant := "burst|" + idScope + "|" + cat + "|" + strconv.FormatInt(bucket, 10)
		idem := idempotencyKey(ch.ID, idScope, variant)
		eid := sample.ID
		nextAttempt := time.Now().UTC()
		action, resumeAt := DecideQuietHours(ch, sample, nextAttempt)
		if action == ActionDeferQuietHours {
			nextAttempt = resumeAt
		}
		_, err := a.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID: ulid.Make().String(), ChannelID: ch.ID, EventID: &eid,
			AggregateKey: idScope + "|burst", IdempotencyKey: idem,
			Status: store.OutboxPending, NextAttemptAt: nextAttempt,
			Title: title, BodyText: body, ParseMode: "HTML",
			HTMLURL: burstURL,
		})
		if err != nil {
			if errors.Is(err, store.ErrConflict) {
				duplicate = true
				continue
			}
			return false, "", err
		}
		enqueued = true
	}
	switch {
	case enqueued:
		return true, "", nil
	case duplicate:
		return false, "duplicate", nil
	default:
		return false, "no_channel", nil
	}
}

// timeBucket 将时间对齐到窗口边界，供多实例 Outbox 幂等键使用。
func timeBucket(t time.Time, window time.Duration) int64 {
	if window <= 0 {
		window = 60 * time.Second
	}
	// 亚秒窗口按毫秒对齐，避免 int64(window/time.Second)==0 除零。
	if window < time.Second {
		ms := window.Milliseconds()
		if ms <= 0 {
			ms = 1
		}
		return t.UnixMilli() / ms
	}
	sec := int64(window / time.Second)
	if sec <= 0 {
		sec = 1
	}
	return t.Unix() / sec
}

// categoryDisplayName 返回类别的显示名（与前端标签一致）。
func categoryDisplayName(cat string) string {
	switch cat {
	case "security":
		return "安全告警"
	case "actions":
		return "Actions"
	case "issue":
		return "Issue"
	case "pr":
		return "PR"
	default:
		return cat
	}
}

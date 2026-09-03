package syncx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// star release 追踪的设置键（system_settings）。
const (
	SettingStarredUsername     = "starred_releases.username"
	SettingStarSyncInterval    = "starred_releases.star_sync_interval"
	SettingReleasePollInterval = "starred_releases.release_poll_interval"
	SettingMaxTrackers         = "starred_releases.max_trackers"
	SettingNotifyPrerelease    = "starred_releases.notify_prerelease"
)

// 默认周期与上限（解析失败一律回退）。
const (
	DefaultStarSyncInterval    = 6 * time.Hour
	DefaultReleasePollInterval = 10 * time.Minute
	DefaultMaxTrackers         = 500
)

const (
	// noReleaseRecheckAfter 无 release 仓库的复查间隔：未来可能开始发 release，不能永久停止。
	noReleaseRecheckAfter = 7 * 24 * time.Hour
	// maxBackfillPerRound 中断补拉时单轮最多补发的新 release 数，防刷屏。
	maxBackfillPerRound = 5
	// maxPollCandidatesPerRound 单轮最多轮询的追踪仓数（分轮防止单轮耗时过长）。
	maxPollCandidatesPerRound = 200
	// maxReleaseNotesStored 事件 PayloadSummary 中保存的 release notes 上限（供更新速览生成输入）。
	maxReleaseNotesStored = 8000
	// trackerListHardLimit 追踪记录列表查询的防御性上限：远超 maxTrackers（500），
	// 仅防病态数据把单轮同步拖垮。
	trackerListHardLimit = 10000
)

// StarredReleasePoller 枚举用户 star 仓库并轮询其最新 release。
// star 列表同步低频（默认 6h，star 行为低频），release 轮询高频（默认 10m，决定通知延迟）。
// 两个周期均从 system_settings 热读取，由 Scheduler 节拍驱动、方法内自判到期。
type StarredReleasePoller struct {
	Store  store.Store
	GitHub *githubx.AppClient
	Public *githubx.PublicClient
	// Engine 可选；注入后新 release 事件经实时通知决策写入 Outbox（不聚合、立即）。
	Engine *rules.Engine
	Logger *slog.Logger

	// mu 串行化 SyncStars/PollReleases：Scheduler 节拍与 HTTP「立即同步」可能并发触发，
	// 无锁会导致 lastStarSync/lastReleasePoll 数据竞争与双重枚举/轮询。
	mu              sync.Mutex
	lastStarSync    time.Time
	lastReleasePoll time.Time
}

// ---- 设置读取 ----

func settingDuration(ctx context.Context, s store.SettingsStore, key string, def, min, max time.Duration) time.Duration {
	if s == nil {
		return def
	}
	row, err := s.Get(ctx, key)
	if err != nil {
		return def
	}
	var raw string
	if err := json.Unmarshal(row.ValueJSON, &raw); err != nil || raw == "" {
		return def
	}
	d, err := time.ParseDuration(raw)
	if err != nil || d < min || d > max {
		return def
	}
	return d
}

func settingInt(ctx context.Context, s store.SettingsStore, key string, def int) int {
	if s == nil {
		return def
	}
	row, err := s.Get(ctx, key)
	if err != nil {
		return def
	}
	var v int
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil || v <= 0 {
		return def
	}
	return v
}

func settingString(ctx context.Context, s store.SettingsStore, key string) string {
	if s == nil {
		return ""
	}
	row, err := s.Get(ctx, key)
	if err != nil {
		return ""
	}
	var v string
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return ""
	}
	return strings.TrimSpace(v)
}

func settingBool(ctx context.Context, s store.SettingsStore, key string) bool {
	if s == nil {
		return false
	}
	row, err := s.Get(ctx, key)
	if err != nil {
		return false
	}
	var v bool
	if err := json.Unmarshal(row.ValueJSON, &v); err != nil {
		return false
	}
	return v
}

// StarSyncInterval star 列表同步周期（1m ~ 30d）。
func StarSyncInterval(ctx context.Context, s store.SettingsStore) time.Duration {
	return settingDuration(ctx, s, SettingStarSyncInterval, DefaultStarSyncInterval, time.Minute, 30*24*time.Hour)
}

// ReleasePollInterval release 轮询周期（1m ~ 24h）。
func ReleasePollInterval(ctx context.Context, s store.SettingsStore) time.Duration {
	return settingDuration(ctx, s, SettingReleasePollInterval, DefaultReleasePollInterval, time.Minute, 24*time.Hour)
}

// MaxTrackers 追踪上限。
func MaxTrackers(ctx context.Context, s store.SettingsStore) int {
	return settingInt(ctx, s, SettingMaxTrackers, DefaultMaxTrackers)
}

// StarredUsername 管理台填写的 GitHub 用户名（匿名枚举公开 star 用）。
func StarredUsername(ctx context.Context, s store.SettingsStore) string {
	return settingString(ctx, s, SettingStarredUsername)
}

// NotifyPrerelease 是否通知预发布版本。
func NotifyPrerelease(ctx context.Context, s store.SettingsStore) bool {
	return settingBool(ctx, s, SettingNotifyPrerelease)
}

// ---- star 列表同步 ----

// SyncStars 按周期自判执行：未到期直接返回（供 Scheduler 节拍调用）。
// 枚举用户公开 star → fork/archived 预过滤 → 新仓注册追踪并做首次基线探测；
// 完整拉全分页后才执行 unstar 移除（中途限流不误删）。
func (p *StarredReleasePoller) SyncStars(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if p.Store == nil {
		return nil
	}
	if !p.lastStarSync.IsZero() && now.Sub(p.lastStarSync) < StarSyncInterval(ctx, p.Store.Settings()) {
		return nil
	}
	return p.syncStarsLocked(ctx)
}

// SyncStarsNow 强制立即执行一轮 star 同步（绕过周期自判），
// 供 HTTP「立即同步」与保存用户名触发：手动操作必须立即可见，不能被定时周期拦住。
func (p *StarredReleasePoller) SyncStarsNow(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.Store == nil {
		return nil
	}
	return p.syncStarsLocked(ctx)
}

// syncStarsLocked 执行 star 枚举与注册（须在持锁下调用）；末尾按结果推进 lastStarSync：
// 成功/确定性跳过推进（避免空转与刷屏），临时失败不推进（下轮节拍快速重试）。
func (p *StarredReleasePoller) syncStarsLocked(ctx context.Context) error {
	now := time.Now().UTC()
	username := StarredUsername(ctx, p.Store.Settings())
	if username == "" {
		// 未配置：推进记账避免每 1m 空转，6h 后再检查。
		p.lastStarSync = now
		p.debug("star sync skipped", "reason", "username_not_set")
		return nil
	}
	client := p.Public
	if client == nil {
		client = &githubx.PublicClient{}
	}
	max := MaxTrackers(ctx, p.Store.Settings())
	seen := make(map[string]bool)
	added := 0
	full := true
	// 全轮共享一次令牌获取：逐仓 register→probe 各自取令牌会每仓查一次 installations 表。
	token := p.installationToken(ctx, "star sync")
	// 追踪计数基数全轮只查一次：上限判定 = 基数 + 本轮新增，负值表示计数失败放行
	//（与原 underLimit「计数失败放行避免误伤采集」语义一致）。
	trackerBase := -1
	if counts, err := p.Store.StarredTrackers().CountByState(ctx); err != nil {
		p.warn("star sync count failed, allow register", "error_code", "tracker_count_failed", "error", err.Error())
	} else {
		trackerBase = 0
		for k, v := range counts {
			if k != store.TrackerStateDisabled {
				trackerBase += v
			}
		}
	}
	// 一次性加载现有追踪记录建 full_name→tracker 映射，避免每仓 GetByFullName 的 N+1 查询
	//（500 追踪上限用户一轮同步即数百次单查）。加载失败必须中止整轮：空映射会把存量
	// tracker 误判为新仓，registerIfNew 的 Upsert 将强制写回 tracking 并清零游标
	//（静默重置用户停用与复查状态）；不推进记账，下个节拍快速重试。
	trackerMap := make(map[string]store.StarredRepoTracker)
	if candidates, err := p.Store.StarredTrackers().ListAll(ctx, trackerListHardLimit); err != nil {
		p.warn("star sync tracker map load failed, abort round", "error_code", "tracker_map_load_failed", "error", err.Error())
		// 与 ListUserStarred 失败同类：返回 err（调度器与手动同步可见本轮失败），不推进记账。
		return err
	} else {
		for _, tk := range candidates {
			trackerMap[tk.FullName] = tk
		}
	}
	for page := 1; ; page++ {
		if err := ctx.Err(); err != nil {
			return err
		}
		if page > max/100+2 {
			// 防御：超出上限页码数后中止，避免异常分页死循环。
			full = false
			break
		}
		items, link, _, err := client.ListUserStarred(ctx, username, page)
		if err != nil {
			if githubx.IsRateLimited(err) {
				// 限流：推进记账避免刷屏，6h 后再试（限流窗口远小于此）。
				p.lastStarSync = now
				p.warn("star sync rate limited, stop round", "error_code", "rate_limited_round_stopped")
				return nil
			}
			// 临时故障：不推进记账，下轮 Scheduler 节拍快速重试。
			p.warn("star sync list failed", "page", page, "error_code", "star_sync_list_failed", "error", err.Error())
			return err
		}
		for _, it := range items {
			if it.Fork || it.Archived {
				continue // fork/archived 零成本预过滤
			}
			seen[it.FullName] = true
			if err := p.registerIfNew(ctx, it.FullName, max, &added, trackerMap, trackerBase, token); err != nil {
				p.warn("star sync register failed", "repo", it.FullName, "error_code", "star_register_failed", "error", err.Error())
			}
		}
		if link == "" {
			break // 末页（以 Link 头为准，中间页条数不应成为 break 依据）
		}
	}
	if full {
		p.removeUnstarred(ctx, seen)
	}
	// 完整成功（含确定性跳过）后才推进记账：失败路径下轮立即重试。
	p.lastStarSync = time.Now().UTC()
	p.debug("star sync ok", "username", username, "seen", len(seen), "added", added)
	return nil
}

// registerIfNew 新 star 仓注册追踪并做首次 release 基线探测；
// 已 inactive 的仓触发复查探测：到复查时间，或带 release 游标（多为 304 误判，立即自愈）。
// trackerMap 为 syncStarsLocked 预加载的全量 full_name→tracker 映射（消除逐仓单查）；
// 注册成功后同步写入该映射，保证同一轮内重复出现的 full_name 幂等。
// trackerBase/token 均为全轮预取：上限判定不再逐仓查 CountByState，
// 探测不再逐仓查 installations 表；trackerBase 为负表示计数失败放行。
func (p *StarredReleasePoller) registerIfNew(ctx context.Context, fullName string, max int, added *int, trackerMap map[string]store.StarredRepoTracker, trackerBase int, token string) error {
	tracker, ok := trackerMap[fullName]
	if ok {
		if tracker.State == store.TrackerStateInactive && tracker.NoReleaseRecheckAt != nil &&
			(tracker.LastReleaseID > 0 || time.Now().UTC().After(*tracker.NoReleaseRecheckAt)) {
			// 有 release 游标却 inactive：多为条件请求 304 误判（release 未变化被当成无 release），
			// 或 release 被整体删除；同步时立即重新探测自愈，不必等 7 天复查。
			// 无游标的真无 release 仓仍按复查周期。
			return p.probeRelease(ctx, token, tracker.ID, fullName)
		}
		return nil
	}
	if trackerBase >= 0 && trackerBase+*added >= max {
		p.warn("star tracker limit reached, skip", "repo", fullName, "max", max, "error_code", "tracker_limit_reached")
		return nil
	}
	now := time.Now().UTC()
	tracker = store.StarredRepoTracker{
		ID: ulid.Make().String(), FullName: fullName, State: store.TrackerStateTracking,
		FirstSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := p.Store.StarredTrackers().Upsert(ctx, tracker); err != nil {
		return err
	}
	trackerMap[fullName] = tracker
	*added++
	// 首次探测：拉一次 release 判断基线或 inactive（不通知，避免历史洪泛）。
	return p.probeRelease(ctx, token, tracker.ID, fullName)
}

// probeRelease 对追踪仓做一次 release 探测：决定基线或 inactive（不建事件）。
// token 由调用方全轮预取（空串表示令牌不可用，原因已在获取处留痕）。
func (p *StarredReleasePoller) probeRelease(ctx context.Context, token, id, fullName string) error {
	if token == "" {
		return nil // 原因已留痕
	}
	owner, name := splitFullName(fullName)
	items, newEtag, modified, _, err := p.GitHub.ListReleases(ctx, token, owner, name, 1, "")
	if err != nil {
		var stErr *githubx.HTTPStatusError
		if errors.As(err, &stErr) && (stErr.StatusCode == http.StatusNotFound || stErr.StatusCode == http.StatusGone) {
			if err := p.Store.StarredTrackers().UpdateState(ctx, id, store.TrackerStateUnavailable); err != nil {
				// 状态推进失败会让已删仓反复轮询 404，Warn 留痕便于排查。
				p.warn("probe state update failed", "repo", fullName, "error_code", "tracker_state_failed", "error", err.Error())
			}
		}
		return err
	}
	if !modified || len(items) == 0 {
		// 从未发布 release：inactive + 7 天复查。
		recheck := time.Now().UTC().Add(noReleaseRecheckAfter)
		return p.Store.StarredTrackers().UpdateNoRelease(ctx, id, recheck)
	}
	rel := items[0]
	published := rel.PublishedAt
	if err := p.Store.StarredTrackers().UpdatePollResult(ctx, id, newEtag, rel.ID, rel.TagName, &published); err != nil {
		return err
	}
	return p.Store.StarredTrackers().UpdateState(ctx, id, store.TrackerStateTracking)
}

// removeUnstarred 完整分页拉全后，将不在 star 列表中的 tracking 仓停用（保留记录）。
func (p *StarredReleasePoller) removeUnstarred(ctx context.Context, seen map[string]bool) {
	candidates, err := p.Store.StarredTrackers().ListPollCandidates(ctx, trackerListHardLimit)
	if err != nil {
		// 查询失败时本轮 unstar 移除整体跳过：必须留痕，否则数据库抖动期间
		// 「已 unstar 但追踪未停用」无从定位。
		p.warn("unstar candidate list failed, skip round", "error_code", "tracker_list_failed", "error", err.Error())
		return
	}
	for _, tk := range candidates {
		if seen[tk.FullName] {
			continue
		}
		if err := p.Store.StarredTrackers().UpdateState(ctx, tk.ID, store.TrackerStateDisabled); err != nil {
			p.warn("unstar disable failed", "repo", tk.FullName, "error_code", "tracker_state_failed", "error", err.Error())
		} else {
			p.debug("tracker disabled on unstar", "repo", tk.FullName)
		}
	}
}

// ---- release 轮询 ----

// PollReleases 按周期自判执行：未到期直接返回。
// 对 tracking 仓以 ETag 条件请求轮询最新 release；新 release 事件化（走通知管线）。
func (p *StarredReleasePoller) PollReleases(ctx context.Context) error {
	p.mu.Lock()
	defer p.mu.Unlock()
	now := time.Now().UTC()
	if p.Store == nil {
		return nil
	}
	if !p.lastReleasePoll.IsZero() && now.Sub(p.lastReleasePoll) < ReleasePollInterval(ctx, p.Store.Settings()) {
		return nil
	}

	if p.GitHub == nil || !p.GitHub.Configured() {
		// 未配置：推进记账避免每 1m 空转（重开后按周期自愈）。
		p.lastReleasePoll = now
		p.debug("release poll skipped", "reason", "app_not_configured")
		return nil
	}
	// feature 关闭时不推进记账：重开后下个节拍立即恢复轮询。
	if !store.LoadFeatureFlags(ctx, p.Store.Settings()).StarredReleases {
		p.debug("release poll skipped", "reason", "feature_disabled")
		return nil
	}
	candidates, err := p.Store.StarredTrackers().ListPollCandidates(ctx, maxPollCandidatesPerRound)
	if err != nil {
		return err
	}
	// 无候选时不推进记账：新仓注册后下个节拍立即接管。
	if len(candidates) == 0 {
		p.debug("release poll skipped", "reason", "no_trackers")
		return nil
	}
	notifyPrerelease := NotifyPrerelease(ctx, p.Store.Settings())
	token := p.installationToken(ctx, "release poll")
	if token == "" {
		// 令牌不可用（多为 App 未配置/无安装的确定性状态）：推进记账避免每 1m 刷屏。
		p.lastReleasePoll = now
		return nil // 原因已留痕
	}
	backfill := 0
	for _, tk := range candidates {
		if err := ctx.Err(); err != nil {
			return err
		}
		if backfill >= maxBackfillPerRound {
			break
		}
		owner, name := splitFullName(tk.FullName)
		// 分页扫描最新 release：从第 1 页起逐页翻到「已覆盖全部比游标新的 release」为止
		//（命中游标所在页，或返回页不满一页即末页）。条件请求 ETag 仅对第 1 页生效。
		page := 1
		var newest githubx.ReleaseItem
		page1Etag := ""
		newestSet := false
		cursorUpdated := false
		advance := true
	pollRepo:
		for {
			items, newEtag, modified, _, err := p.GitHub.ListReleases(ctx, token, owner, name, page, tk.ETag)
			if err != nil {
				var stErr *githubx.HTTPStatusError
				switch {
				case githubx.IsRateLimited(err):
					// 限流：推进记账避免刷屏，下个周期再试。
					p.lastReleasePoll = now
					p.warn("release poll rate limited, stop round", "error_code", "rate_limited_round_stopped")
					return nil
				case errors.As(err, &stErr) && (stErr.StatusCode == http.StatusNotFound || stErr.StatusCode == http.StatusGone):
					// 删仓/转私有：标记不可用并停止轮询（star 同步发现恢复时转回）。
					if err := p.Store.StarredTrackers().UpdateState(ctx, tk.ID, store.TrackerStateUnavailable); err != nil {
						p.warn("release poll state update failed", "repo", tk.FullName, "error_code", "tracker_state_failed", "error", err.Error())
					}
					break pollRepo
				default:
					// 超时/5xx 等临时故障：保持现状等待下轮重试；翻页失败不推进游标（防漏 release）。
					p.warn("release poll failed", "repo", tk.FullName, "page", page, "error_code", "release_poll_failed", "error", err.Error())
					advance = false
					break pollRepo
				}
			}
			if page == 1 {
				page1Etag = newEtag
				if !modified {
					// 304：未变更，仅推进轮询时间。304 响应体为空（items 为空是正常表示），
					// 必须先于空列表判定处理，否则会把有 release 的仓误标「无 release」。
					if err := p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, tk.LastReleaseID, tk.LastReleaseTag, tk.LastReleasePublishedAt); err != nil {
						// ETag 落库失败 → 下轮全量拉取 release body（浪费带宽），Debug 留痕。
						p.debug("release poll cursor update failed", "repo", tk.FullName, "error_code", "tracker_cursor_failed", "error", err.Error())
					}
					cursorUpdated = true
					p.debug("release poll ok", "repo", tk.FullName, "modified", false)
					break pollRepo
				}
			}
			if len(items) == 0 {
				if page == 1 {
					// 曾追踪过 release 的仓现在为空（异常）：按无 release 处理并复查。
					recheck := time.Now().UTC().Add(noReleaseRecheckAfter)
					if err := p.Store.StarredTrackers().UpdateNoRelease(ctx, tk.ID, recheck); err != nil {
						p.warn("release poll state update failed", "repo", tk.FullName, "error_code", "tracker_state_failed", "error", err.Error())
					}
				}
				break pollRepo
			}
			if page == 1 {
				// 基线：首次见到 release 只记游标，不通知（避免历史洪泛）。
				if tk.LastReleaseID == 0 {
					published := items[0].PublishedAt
					if err := p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, items[0].ID, items[0].TagName, &published); err != nil {
						p.debug("release poll cursor update failed", "repo", tk.FullName, "error_code", "tracker_cursor_failed", "error", err.Error())
					}
					cursorUpdated = true
					p.debug("release baseline recorded", "repo", tk.FullName, "release_id", items[0].ID)
					break pollRepo
				}
				newest = items[0]
				newestSet = true
			}
			// 本页逐条事件化（补拉）：比游标新的 release 全部处理，避免中间版本静默丢失。
			// 预发布且未开启通知：不建事件但游标照常推进（正式版发布时再通知）。
			// 达到本轮补发上限或事件落库失败时，不推进游标与 ETag，下轮节拍从旧游标续补
			//（事件指纹幂等，重复扫描安全，不会重复投递）。
			for _, rel := range items {
				if rel.ID <= tk.LastReleaseID {
					break // 已处理过的 release 不再回溯
				}
				if rel.Prerelease && !notifyPrerelease {
					continue
				}
				created, err := p.createReleaseEvent(ctx, tk.FullName, rel)
				if err != nil {
					p.warn("release event create failed", "repo", tk.FullName, "release_id", rel.ID, "error_code", "event_create_failed", "error", err.Error())
					advance = false
					break pollRepo
				}
				if !created {
					continue // 已存在（前轮已建事件但游标未推进），不计入补发预算
				}
				backfill++
				p.debug("release event created", "repo", tk.FullName, "tag", rel.TagName, "release_id", rel.ID)
				if backfill >= maxBackfillPerRound {
					advance = false
					p.debug("release poll backfill cap reached", "repo", tk.FullName, "cap", maxBackfillPerRound)
					break pollRepo
				}
			}
			// 翻页终止：本页已含游标（后续页都是已处理 release），或本页为末页（不足一页）。
			if items[len(items)-1].ID <= tk.LastReleaseID || len(items) < githubx.ReleaseListPerPage {
				break pollRepo
			}
			page++
		}
		// 游标推进到最新 release：仅在完整覆盖（无失败/无 cap/无 304 与基线分支）时执行。
		if !advance || cursorUpdated || !newestSet {
			continue
		}
		published := newest.PublishedAt
		if err := p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, page1Etag, newest.ID, newest.TagName, &published); err != nil {
			p.debug("release poll cursor update failed", "repo", tk.FullName, "error_code", "tracker_cursor_failed", "error", err.Error())
		}
		p.debug("release poll ok", "repo", tk.FullName, "tag", newest.TagName, "event_created", true)
	}
	// 完整轮询成功后才推进记账：失败/限流路径下轮节拍快速重试。
	p.lastReleasePoll = time.Now().UTC()
	return nil
}

// createReleaseEvent 将新 release 事件化并落库（指纹幂等，重复轮询不重复通知），
// 随后触发实时通知决策（rules.Engine 直连，不聚合：release 低频单条，避免 60s 窗口延迟）。
// 返回是否新建了事件：幂等命中（已存在）时返回 false,nil，
// 供补拉循环区分「已处理过」与「新事件」，避免重复消耗单轮补发预算。
func (p *StarredReleasePoller) createReleaseEvent(ctx context.Context, fullName string, rel githubx.ReleaseItem) (bool, error) {
	stateHash := fmt.Sprintf("id:%d", rel.ID)
	fp := normalizer.Fingerprint(
		"starred_releases", fullName, store.ReleaseKind,
		normalizer.ResourceIdentity(store.ReleaseKind, rel.ID, 0),
		"published", rel.PublishedAt, stateHash,
	)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return false, nil // 已存在（幂等）
	}
	num := rel.ID
	src := rel.PublishedAt
	title := rel.Name
	if title == "" {
		title = rel.TagName
	}
	notes := rel.Body
	if len(notes) > maxReleaseNotesStored {
		notes = notes[:maxReleaseNotesStored]
	}
	// 外部 star 仓不建 Repository 行（tracker 独立表），事件无法挂 RepositoryID；
	// 仓库名写入 PayloadSummary，供每日摘要预览与 AI 总结回退引用，避免 release 事件丢失归属。
	ev := store.Event{
		ID: ulid.Make().String(), Source: "starred_releases", Kind: store.ReleaseKind, Action: "published",
		Title: title, SubjectNumber: &num, Actor: rel.Author.Login,
		OccurredAt: rel.PublishedAt, SourceUpdatedAt: &src, HTMLURL: rel.HTMLURL,
		DedupeFingerprint: fp, StateHash: stateHash,
		PayloadSummary: map[string]any{
			"tag_name": rel.TagName, "prerelease": rel.Prerelease, "notes": notes,
			"repository": fullName,
		},
	}
	if _, err := p.Store.Events().Create(ctx, ev); err != nil {
		return false, err
	}
	if p.Engine != nil {
		// Outbox 幂等键以 event_id 派生，重复 Evaluate 不会重复投递。
		if err := p.Engine.Evaluate(ctx, normalizer.Result{Event: &ev}, fullName); err != nil {
			p.warn("release notify evaluate failed", "repo", fullName, "error_code", "notify_evaluate_failed", "error", err.Error())
		}
	}
	return true, nil
}

// installationToken 取任一安装的令牌；App 未配置或无安装时留痕并返回空串。
// scene 标注调用场景（"star sync" / "release poll"），避免 star 同步探测阶段的
// 告警日志被误读为 release 轮询问题。
func (p *StarredReleasePoller) installationToken(ctx context.Context, scene string) string {
	if p.GitHub == nil || !p.GitHub.Configured() {
		p.warn(scene+" skipped", "reason", "app_not_configured", "error_code", "app_not_configured")
		return ""
	}
	installations, err := p.Store.Installations().List(ctx)
	if err != nil {
		p.warn(scene+" skipped", "reason", "installation_list_failed", "error_code", "installation_list_failed", "error", err.Error())
		return ""
	}
	if len(installations) == 0 {
		p.warn(scene+" skipped", "reason", "no_installation", "error_code", "no_installation")
		return ""
	}
	token, err := p.GitHub.InstallationToken(ctx, installations[0].InstallationID)
	if err != nil {
		p.warn("installation token failed", "installation_id", installations[0].InstallationID, "error_code", "installation_token_failed", "error", err.Error())
		return ""
	}
	return token
}

func splitFullName(fullName string) (owner, name string) {
	parts := strings.SplitN(fullName, "/", 2)
	if len(parts) != 2 {
		return fullName, ""
	}
	return parts[0], parts[1]
}

func (p *StarredReleasePoller) debug(msg string, args ...any) {
	if p.Logger != nil {
		p.Logger.Debug(msg, args...)
	}
}

func (p *StarredReleasePoller) warn(msg string, args ...any) {
	if p.Logger != nil {
		p.Logger.Warn(msg, args...)
	}
}

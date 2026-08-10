package syncx

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
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
	// maxReleaseNotesStored 事件 PayloadSummary 中保存的 release notes 上限（供 AI 总结输入）。
	maxReleaseNotesStored = 8000
)

// StarredReleasePoller 枚举用户 star 仓库并轮询其最新 release。
// star 列表同步低频（默认 6h，star 行为低频），release 轮询高频（默认 10m，决定通知延迟）。
// 两个周期均从 system_settings 热读取，由 Scheduler 节拍驱动、方法内自判到期。
type StarredReleasePoller struct {
	Store  store.Store
	GitHub *githubx.AppClient
	Public *githubx.PublicClient
	Logger *slog.Logger

	// lastStarSync / lastReleasePoll 内存记账：进程重启后零值立即触发首轮。
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

// SyncStars 按周期自判执行：未到期直接返回。
// 枚举用户公开 star → fork/archived 预过滤 → 新仓注册追踪并做首次基线探测；
// 完整拉全分页后才执行 unstar 移除（中途限流不误删）。
func (p *StarredReleasePoller) SyncStars(ctx context.Context) error {
	now := time.Now().UTC()
	if p.Store == nil {
		return nil
	}
	if !p.lastStarSync.IsZero() && now.Sub(p.lastStarSync) < StarSyncInterval(ctx, p.Store.Settings()) {
		return nil
	}
	p.lastStarSync = now

	username := StarredUsername(ctx, p.Store.Settings())
	if username == "" {
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
	for page := 1; ; page++ {
		if page > max/100+2 {
			// 防御：超出上限页码数后中止，避免异常分页死循环。
			full = false
			break
		}
		items, link, _, err := client.ListUserStarred(ctx, username, page)
		if err != nil {
			if githubx.IsRateLimited(err) {
				p.warn("star sync rate limited, stop round", "error_code", "rate_limited_round_stopped")
				return nil
			}
			p.warn("star sync list failed", "page", page, "error_code", "star_sync_list_failed", "error", err.Error())
			return err
		}
		for _, it := range items {
			if it.Fork || it.Archived {
				continue // fork/archived 零成本预过滤
			}
			seen[it.FullName] = true
			if err := p.registerIfNew(ctx, it.FullName, max, &added); err != nil {
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
	p.debug("star sync ok", "username", username, "seen", len(seen), "added", added)
	return nil
}

// registerIfNew 新 star 仓注册追踪并做首次 release 基线探测；
// 已 inactive 且到复查时间的仓触发一次复查探测。
func (p *StarredReleasePoller) registerIfNew(ctx context.Context, fullName string, max int, added *int) error {
	tracker, err := p.Store.StarredTrackers().GetByFullName(ctx, fullName)
	if err == nil {
		if tracker.State == store.TrackerStateInactive && tracker.NoReleaseRecheckAt != nil &&
			time.Now().UTC().After(*tracker.NoReleaseRecheckAt) {
			return p.probeRelease(ctx, tracker.ID, fullName)
		}
		return nil
	}
	if !errors.Is(err, store.ErrNotFound) {
		return err
	}
	if !p.underLimit(ctx, max, *added) {
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
	*added++
	// 首次探测：拉一次 release 判断基线或 inactive（不通知，避免历史洪泛）。
	return p.probeRelease(ctx, tracker.ID, fullName)
}

// underLimit 判定当前追踪数（disabled 不计）加本轮新增是否未超上限。
func (p *StarredReleasePoller) underLimit(ctx context.Context, max, added int) bool {
	counts, err := p.Store.StarredTrackers().CountByState(ctx)
	if err != nil {
		return true // 计数失败时放行，避免误伤采集
	}
	total := added
	for k, v := range counts {
		if k != store.TrackerStateDisabled {
			total += v
		}
	}
	return total < max
}

// probeRelease 对追踪仓做一次 release 探测：决定基线或 inactive（不建事件）。
func (p *StarredReleasePoller) probeRelease(ctx context.Context, id, fullName string) error {
	token := p.installationToken(ctx)
	if token == "" {
		return nil // 原因已留痕
	}
	owner, name := splitFullName(fullName)
	items, newEtag, modified, _, err := p.GitHub.ListReleases(ctx, token, owner, name, "")
	if err != nil {
		var stErr *githubx.HTTPStatusError
		if errors.As(err, &stErr) && (stErr.StatusCode == http.StatusNotFound || stErr.StatusCode == http.StatusGone) {
			_ = p.Store.StarredTrackers().UpdateState(ctx, id, store.TrackerStateUnavailable)
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
	candidates, err := p.Store.StarredTrackers().ListPollCandidates(ctx, 10000)
	if err != nil {
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
	now := time.Now().UTC()
	if p.Store == nil {
		return nil
	}
	if !p.lastReleasePoll.IsZero() && now.Sub(p.lastReleasePoll) < ReleasePollInterval(ctx, p.Store.Settings()) {
		return nil
	}
	p.lastReleasePoll = now

	if p.GitHub == nil || !p.GitHub.Configured() {
		p.debug("release poll skipped", "reason", "app_not_configured")
		return nil
	}
	if !store.LoadFeatureFlags(ctx, p.Store.Settings()).StarredReleases {
		p.debug("release poll skipped", "reason", "feature_disabled")
		return nil
	}
	candidates, err := p.Store.StarredTrackers().ListPollCandidates(ctx, maxPollCandidatesPerRound)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		p.debug("release poll skipped", "reason", "no_trackers")
		return nil
	}
	notifyPrerelease := NotifyPrerelease(ctx, p.Store.Settings())
	token := p.installationToken(ctx)
	if token == "" {
		return nil // 原因已留痕
	}
	backfill := 0
	for _, tk := range candidates {
		if backfill >= maxBackfillPerRound {
			break
		}
		owner, name := splitFullName(tk.FullName)
		items, newEtag, modified, _, err := p.GitHub.ListReleases(ctx, token, owner, name, tk.ETag)
		if err != nil {
			var stErr *githubx.HTTPStatusError
			switch {
			case githubx.IsRateLimited(err):
				p.warn("release poll rate limited, stop round", "error_code", "rate_limited_round_stopped")
				return nil
			case errors.As(err, &stErr) && (stErr.StatusCode == http.StatusNotFound || stErr.StatusCode == http.StatusGone):
				// 删仓/转私有：标记不可用并停止轮询（star 同步发现恢复时转回）。
				_ = p.Store.StarredTrackers().UpdateState(ctx, tk.ID, store.TrackerStateUnavailable)
				continue
			default:
				// 超时/5xx 等临时故障：保持现状等待下轮重试。
				p.warn("release poll failed", "repo", tk.FullName, "error_code", "release_poll_failed", "error", err.Error())
				continue
			}
		}
		if !modified {
			// 304：未变更，仅推进轮询时间。
			_ = p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, tk.LastReleaseID, tk.LastReleaseTag, tk.LastReleasePublishedAt)
			p.debug("release poll ok", "repo", tk.FullName, "modified", false)
			continue
		}
		if len(items) == 0 {
			// 曾追踪过 release 的仓现在为空（异常）：按无 release 处理并复查。
			recheck := time.Now().UTC().Add(noReleaseRecheckAfter)
			_ = p.Store.StarredTrackers().UpdateNoRelease(ctx, tk.ID, recheck)
			continue
		}
		rel := items[0]
		published := rel.PublishedAt
		// 基线：首次见到 release 只记游标，不通知（避免历史洪泛）。
		if tk.LastReleaseID == 0 {
			_ = p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, rel.ID, rel.TagName, &published)
			p.debug("release baseline recorded", "repo", tk.FullName, "release_id", rel.ID)
			continue
		}
		if rel.ID == tk.LastReleaseID {
			_ = p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, rel.ID, rel.TagName, &published)
			continue
		}
		// 预发布且未开启通知：只推进游标，正式版发布时再通知。
		if rel.Prerelease && !notifyPrerelease {
			_ = p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, rel.ID, rel.TagName, &published)
			p.debug("release prerelease skipped", "repo", tk.FullName, "tag", rel.TagName)
			continue
		}
		if err := p.createReleaseEvent(ctx, tk.FullName, rel); err != nil {
			p.warn("release event create failed", "repo", tk.FullName, "error_code", "event_create_failed", "error", err.Error())
			continue
		}
		backfill++
		_ = p.Store.StarredTrackers().UpdatePollResult(ctx, tk.ID, newEtag, rel.ID, rel.TagName, &published)
		p.debug("release poll ok", "repo", tk.FullName, "tag", rel.TagName, "event_created", true)
	}
	return nil
}

// createReleaseEvent 将新 release 事件化并落库（指纹幂等，重复轮询不重复通知）。
func (p *StarredReleasePoller) createReleaseEvent(ctx context.Context, fullName string, rel githubx.ReleaseItem) error {
	stateHash := fmt.Sprintf("id:%d", rel.ID)
	fp := normalizer.Fingerprint(
		"starred_releases", fullName, store.ReleaseKind,
		normalizer.ResourceIdentity(store.ReleaseKind, int(rel.ID), 0),
		"published", rel.PublishedAt, stateHash,
	)
	if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
		return nil // 已存在（幂等）
	}
	num := int(rel.ID)
	src := rel.PublishedAt
	title := rel.Name
	if title == "" {
		title = rel.TagName
	}
	notes := rel.Body
	if len(notes) > maxReleaseNotesStored {
		notes = notes[:maxReleaseNotesStored]
	}
	_, err := p.Store.Events().Create(ctx, store.Event{
		ID: ulid.Make().String(), Source: "starred_releases", Kind: store.ReleaseKind, Action: "published",
		Title: title, SubjectNumber: &num, Actor: rel.Author.Login,
		OccurredAt: rel.PublishedAt, SourceUpdatedAt: &src, HTMLURL: rel.HTMLURL,
		DedupeFingerprint: fp, StateHash: stateHash,
		PayloadSummary: map[string]any{
			"tag_name": rel.TagName, "prerelease": rel.Prerelease, "notes": notes,
		},
	})
	return err
}

// installationToken 取任一安装的令牌；App 未配置或无安装时留痕并返回空串。
func (p *StarredReleasePoller) installationToken(ctx context.Context) string {
	if p.GitHub == nil || !p.GitHub.Configured() {
		p.warn("release poll skipped", "reason", "app_not_configured", "error_code", "app_not_configured")
		return ""
	}
	installations, err := p.Store.Installations().List(ctx)
	if err != nil || len(installations) == 0 {
		p.warn("release poll skipped", "reason", "no_installation", "error_code", "no_installation")
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

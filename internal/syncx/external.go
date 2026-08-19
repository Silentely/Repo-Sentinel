package syncx

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"strconv"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ExternalPoller 外部公开仓 Issues 轮询。
type ExternalPoller struct {
	Store  store.Store
	Client *githubx.PublicClient
	Logger *slog.Logger
	// Interval 仅作文档；实际由 Scheduler 调度
}

// PollOne 轮询单个外部仓。
func (p *ExternalPoller) PollOne(ctx context.Context, repo store.Repository) error {
	if repo.Type != store.RepositoryTypeExternal {
		return nil
	}
	// 监控总开关关闭或仓库已归档：停止采集，与设置页开关语义一致。
	if !repo.MonitorEnabled || repo.IsArchived || repo.SyncStatus == store.SyncStatusArchived {
		return nil
	}
	if p.Client == nil {
		p.Client = &githubx.PublicClient{}
	}
	isBaseline := repo.SyncStatus == store.SyncStatusBaseline
	var since *time.Time
	if !isBaseline {
		if cur, err := p.Store.Cursors().Get(ctx, repo.ID, "issues"); err == nil && cur.CursorValue != "" {
			if t, e := time.Parse(time.RFC3339, cur.CursorValue); e == nil {
				t = t.Add(-30 * time.Second)
				since = &t
			}
		}
	}
	items, remaining, err := p.Client.ListPublicIssues(ctx, repo.Owner, repo.Name, since, 1)
	if err != nil {
		// 仅 404/410 说明仓转私有或已删除，标记不可用并暂停轮询；
		// 超时/限流/5xx 是临时故障，保持现状等待下轮重试。
		var stErr *githubx.HTTPStatusError
		if errors.As(err, &stErr) && (stErr.StatusCode == http.StatusNotFound || stErr.StatusCode == http.StatusGone) {
			if uerr := p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusUnavailable); uerr != nil && p.Logger != nil {
				// 状态推进失败会留下「GitHub 已删/转私有但本地仍正常轮询」的不一致，Warn 留痕。
				p.Logger.Warn("external poll state update failed",
					"repo", repo.FullName, "error_code", "repo_state_update_failed", "error", uerr.Error())
			}
		}
		return err
	}
	if remaining >= 0 && remaining < 20 && p.Logger != nil {
		p.Logger.Warn("public api rate low", "error_code", "public_api_rate_low", "remaining", remaining, "repo", repo.FullName)
	}
	features := store.LoadFeatureFlags(ctx, p.Store.Settings())

	// star 计数快照：公开仓轮询时顺带拉取，失败不阻断。
	if features.Stars && repo.StarsEnabled {
		if meta, _, err := p.Client.GetRepository(ctx, repo.Owner, repo.Name); err != nil {
			if p.Logger != nil {
				p.Logger.Warn("star snapshot poll failed", "repo", repo.FullName, "error_code", "star_snapshot_failed", "error", err.Error())
			}
		} else if meta.StargazersCount > 0 {
			if _, err := p.Store.RepoStatSnapshots().Upsert(ctx, store.RepoStatSnapshot{
				RepositoryID: repo.ID, Metric: "stargazers", Value: meta.StargazersCount,
				SampleDate: time.Now().UTC().Format("2006-01-02"),
			}); err != nil && p.Logger != nil {
				p.Logger.Warn("star snapshot upsert failed", "repo", repo.FullName, "error_code", "star_snapshot_upsert_failed", "error", err.Error())
			}
		}
	}

	for _, it := range items {
		kind := store.WorkItemKindIssue
		if it.PullRequest != nil {
			kind = store.WorkItemKindPR
		}
		// Issues API 混合返回 issue 与 PR：按全局 + 仓库级开关决定采集谁。
		if kind == store.WorkItemKindIssue && !(features.Issues && repo.IssuesEnabled) {
			continue
		}
		if kind == store.WorkItemKindPR && !(features.PullRequests && repo.PrEnabled) {
			continue
		}
		labels := make([]any, 0, len(it.Labels))
		for _, l := range it.Labels {
			labels = append(labels, l.Name)
		}
		hash := normalizer.StateHash(kind, it.State, it.Title, it.User.Login, strconv.FormatBool(it.Draft))
		item := store.WorkItem{
			RepositoryID: repo.ID, Number: it.Number, Kind: kind, State: it.State, Title: it.Title,
			Author: it.User.Login, LabelsJSON: labels, HTMLURL: it.HTMLURL,
			Draft: it.Draft, SourceUpdatedAt: it.UpdatedAt, StateHash: hash,
		}
		saved, updated, err := p.Store.WorkItems().UpsertIfNewer(ctx, item)
		if err != nil || !updated {
			continue
		}
		if isBaseline {
			continue // 基线不发事件洪流
		}
		fp := normalizer.Fingerprint("external_poll", repo.FullName, kind, normalizer.ResourceIdentity(kind, saved.Number, 0), "updated", saved.SourceUpdatedAt, hash)
		if _, err := p.Store.Events().GetByFingerprint(ctx, fp); err == nil {
			continue
		}
		num := int64(saved.Number)
		src := saved.SourceUpdatedAt
		if _, err := p.Store.Events().Create(ctx, store.Event{
			ID: ulid.Make().String(), Source: "external_poll", Kind: kind, Action: "updated",
			RepositoryID: &repo.ID, SubjectNumber: &num, Title: saved.Title, Actor: saved.Author,
			OccurredAt: saved.SourceUpdatedAt, SourceUpdatedAt: &src, HTMLURL: saved.HTMLURL,
			DedupeFingerprint: fp, StateHash: hash, PayloadSummary: map[string]any{"state": saved.State},
		}); err != nil && p.Logger != nil {
			// 事件落库失败意味着通知丢失，必须留痕。
			p.Logger.Warn("external poll event create failed", "repo", repo.FullName, "kind", kind, "error_code", "event_create_failed", "error", err.Error())
		}
	}
	now := time.Now().UTC()
	repo.LastSyncedAt = &now
	if isBaseline {
		repo.SyncStatus = store.SyncStatusActive
		repo.BaselineFinishedAt = &now
	}
	if _, err := p.Store.Repositories().Upsert(ctx, repo); err != nil && p.Logger != nil {
		// 状态推进失败会留下陈旧 sync_status，影响后续调度判断，必须留痕。
		p.Logger.Warn("external repo sync status advance failed", "repo", repo.FullName, "error_code", "repo_upsert_failed", "error", err.Error())
	}
	if _, err := p.Store.Cursors().Upsert(ctx, store.SyncCursor{
		RepositoryID: repo.ID, Resource: "issues", CursorValue: now.Format(time.RFC3339), LastSuccessAt: &now,
	}); err != nil && p.Logger != nil {
		// 游标推进失败会让下次轮询重拉本轮数据（幂等键兜底），记录日志便于排查重复。
		p.Logger.Warn("external issues cursor advance failed", "repo", repo.FullName, "error_code", "cursor_upsert_failed", "error", err.Error())
	}
	if p.Logger != nil {
		// 单仓轮询成功留痕（Debug）：排查"外部仓到底轮询过没有"不必依赖调度成功日志。
		p.Logger.Debug("external poll ok", "repo", repo.FullName, "baseline", isBaseline)
	}
	return nil
}

// PollAll 轮询全部外部仓（最多 MaxExternalRepositories 个）。
func (p *ExternalPoller) PollAll(ctx context.Context) error {
	// 按最后同步时间取候选，保证所有外部仓轮流被轮询。
	repos, err := p.Store.Repositories().ListSyncCandidates(ctx, store.RepositoryTypeExternal, store.MaxExternalRepositories)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		if repo.SyncStatus == store.SyncStatusUnavailable || repo.SyncStatus == store.SyncStatusArchived {
			continue
		}
		if repo.IsArchived {
			// GitHub 侧已归档但本地状态未联动：顺手收口归档。
			archived := true
			_ = p.Store.Repositories().UpdateSettings(ctx, repo.ID, store.RepositorySettings{IsArchived: &archived})
			continue
		}
		if !repo.MonitorEnabled {
			continue
		}
		if err := p.PollOne(ctx, repo); err != nil {
			// 限流是令牌级信号（PAT 配额共享）：停止本轮，避免逐仓连环请求放大次限流。
			if githubx.IsRateLimited(err) {
				if p.Logger != nil {
					p.Logger.Warn("external poll rate limited, stop round", "error_code", "rate_limited_round_stopped", "repo", repo.FullName, "retry_after", githubx.RetryAfterOf(err).String())
				}
				return nil
			}
			if p.Logger != nil {
				p.Logger.Error("external poll failed", "repo", repo.FullName, "error_code", "external_poll_failed")
			}
		}
	}
	return nil
}

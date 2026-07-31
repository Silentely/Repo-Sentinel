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
			_ = p.Store.Repositories().UpdateSyncStatus(ctx, repo.ID, store.SyncStatusUnavailable)
		}
		return err
	}
	if remaining >= 0 && remaining < 20 && p.Logger != nil {
		p.Logger.Warn("public api rate low", "remaining", remaining, "repo", repo.FullName)
	}
	for _, it := range items {
		kind := store.WorkItemKindIssue
		if it.PullRequest != nil {
			kind = store.WorkItemKindPR
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
		num := saved.Number
		src := saved.SourceUpdatedAt
		_, _ = p.Store.Events().Create(ctx, store.Event{
			ID: ulid.Make().String(), Source: "external_poll", Kind: kind, Action: "updated",
			RepositoryID: &repo.ID, SubjectNumber: &num, Title: saved.Title, Actor: saved.Author,
			OccurredAt: saved.SourceUpdatedAt, SourceUpdatedAt: &src, HTMLURL: saved.HTMLURL,
			DedupeFingerprint: fp, StateHash: hash, PayloadSummary: map[string]any{"state": saved.State},
		})
	}
	now := time.Now().UTC()
	repo.LastSyncedAt = &now
	if isBaseline {
		repo.SyncStatus = store.SyncStatusActive
		repo.BaselineFinishedAt = &now
	}
	_, _ = p.Store.Repositories().Upsert(ctx, repo)
	_, _ = p.Store.Cursors().Upsert(ctx, store.SyncCursor{
		RepositoryID: repo.ID, Resource: "issues", CursorValue: now.Format(time.RFC3339), LastSuccessAt: &now,
	})
	return nil
}

// PollAll 轮询全部外部仓（最多 20）。
func (p *ExternalPoller) PollAll(ctx context.Context) error {
	// 按最后同步时间取候选，保证所有外部仓轮流被轮询。
	repos, err := p.Store.Repositories().ListSyncCandidates(ctx, store.RepositoryTypeExternal, 20)
	if err != nil {
		return err
	}
	for _, repo := range repos {
		if repo.SyncStatus == store.SyncStatusUnavailable {
			continue
		}
		if err := p.PollOne(ctx, repo); err != nil && p.Logger != nil {
			p.Logger.Error("external poll failed", "repo", repo.FullName, "error_code", "external_poll_failed")
		}
	}
	return nil
}

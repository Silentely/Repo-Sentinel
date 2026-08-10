package store

import (
	"context"
	"time"

	sql "entgo.io/ent/dialect/sql"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/starredrepotracker"
)

type starredTrackerStore struct{ client *entclient.Client }

// Upsert 按 full_name 幂等写入：已存在则更新，否则创建。
func (s *starredTrackerStore) Upsert(ctx context.Context, in StarredRepoTracker) error {
	now := time.Now().UTC()
	existing, err := s.client.StarredRepoTracker.Query().
		Where(starredrepotracker.FullNameEQ(in.FullName)).
		Only(ctx)
	if err == nil {
		_, err := s.client.StarredRepoTracker.UpdateOneID(existing.ID).
			SetFullName(in.FullName).
			SetState(in.State).
			SetEtag(in.ETag).
			SetLastReleaseID(in.LastReleaseID).
			SetLastReleaseTag(in.LastReleaseTag).
			SetNillableLastReleasePublishedAt(in.LastReleasePublishedAt).
			SetNillableNoReleaseSince(in.NoReleaseSince).
			SetNillableNoReleaseRecheckAt(in.NoReleaseRecheckAt).
			SetNillableLastPollAt(in.LastPollAt).
			SetUpdatedAt(now).
			Save(ctx)
		return mapStoreError(err)
	}
	if mapStoreError(err) != ErrNotFound {
		return mapStoreError(err)
	}
	if in.ID == "" {
		in.ID = newID()
	}
	if in.CreatedAt.IsZero() {
		in.CreatedAt = now
	}
	_, err = s.client.StarredRepoTracker.Create().
		SetID(in.ID).
		SetFullName(in.FullName).
		SetState(in.State).
		SetEtag(in.ETag).
		SetLastReleaseID(in.LastReleaseID).
		SetLastReleaseTag(in.LastReleaseTag).
		SetNillableLastReleasePublishedAt(in.LastReleasePublishedAt).
		SetNillableNoReleaseSince(in.NoReleaseSince).
		SetNillableNoReleaseRecheckAt(in.NoReleaseRecheckAt).
		SetFirstSeenAt(in.FirstSeenAt).
		SetNillableLastPollAt(in.LastPollAt).
		SetCreatedAt(in.CreatedAt).
		SetUpdatedAt(now).
		Save(ctx)
	return mapStoreError(err)
}

func (s *starredTrackerStore) GetByFullName(ctx context.Context, fullName string) (StarredRepoTracker, error) {
	entity, err := s.client.StarredRepoTracker.Query().
		Where(starredrepotracker.FullNameEQ(fullName)).
		Only(ctx)
	if err != nil {
		return StarredRepoTracker{}, mapStoreError(err)
	}
	return starredRepoTrackerFromEntity(entity), nil
}

// ListPollCandidates 返回 state=tracking 的轮询候选，按 last_poll_at 升序（未轮询过优先）。
func (s *starredTrackerStore) ListPollCandidates(ctx context.Context, limit int) ([]StarredRepoTracker, error) {
	entities, err := s.client.StarredRepoTracker.Query().
		Where(starredrepotracker.StateEQ(TrackerStateTracking)).
		Order(starredrepotracker.ByLastPollAt(sql.OrderAsc())).
		Limit(limit).
		All(ctx)
	if err != nil {
		return nil, mapStoreError(err)
	}
	items := make([]StarredRepoTracker, 0, len(entities))
	for _, e := range entities {
		items = append(items, starredRepoTrackerFromEntity(e))
	}
	return items, nil
}

// UpdatePollResult 推进 release 轮询结果并更新 last_poll_at。
func (s *starredTrackerStore) UpdatePollResult(ctx context.Context, id, etag string, lastReleaseID int64, lastReleaseTag string, publishedAt *time.Time) error {
	now := time.Now().UTC()
	_, err := s.client.StarredRepoTracker.UpdateOneID(id).
		SetEtag(etag).
		SetLastReleaseID(lastReleaseID).
		SetLastReleaseTag(lastReleaseTag).
		SetNillableLastReleasePublishedAt(publishedAt).
		SetLastPollAt(now).
		SetUpdatedAt(now).
		Save(ctx)
	return mapStoreError(err)
}

// UpdateNoRelease 标记仓库无 release（进入 inactive），并设置下次复查时间。
func (s *starredTrackerStore) UpdateNoRelease(ctx context.Context, id string, recheckAt time.Time) error {
	now := time.Now().UTC()
	_, err := s.client.StarredRepoTracker.UpdateOneID(id).
		SetState(TrackerStateInactive).
		SetNoReleaseSince(now).
		SetNoReleaseRecheckAt(recheckAt).
		SetUpdatedAt(now).
		Save(ctx)
	return mapStoreError(err)
}

func (s *starredTrackerStore) UpdateState(ctx context.Context, id, state string) error {
	now := time.Now().UTC()
	_, err := s.client.StarredRepoTracker.UpdateOneID(id).
		SetState(state).
		SetUpdatedAt(now).
		Save(ctx)
	return mapStoreError(err)
}

// CountByState 按 state 统计数量。
func (s *starredTrackerStore) CountByState(ctx context.Context) (map[string]int, error) {
	var rows []struct {
		State string
		Count int
	}
	if err := s.client.StarredRepoTracker.Query().
		GroupBy(starredrepotracker.FieldState).
		Aggregate(func(s *sql.Selector) string { return sql.Count("*") }).
		Scan(ctx, &rows); err != nil {
		return nil, mapStoreError(err)
	}
	counts := make(map[string]int, len(rows))
	for _, r := range rows {
		counts[r.State] = r.Count
	}
	return counts, nil
}

// List 管理台分页列表；f.State 精确筛选。
func (s *starredTrackerStore) List(ctx context.Context, f ListFilter) ([]StarredRepoTracker, PageResult, error) {
	f = NormalizeListFilter(f)
	q := s.client.StarredRepoTracker.Query()
	if f.State != "" {
		q = q.Where(starredrepotracker.StateEQ(f.State))
	}
	total, err := q.Count(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	entities, err := q.
		Order(starredrepotracker.ByFirstSeenAt(sql.OrderDesc())).
		Offset((f.Page - 1) * f.PerPage).
		Limit(f.PerPage).
		All(ctx)
	if err != nil {
		return nil, PageResult{}, mapStoreError(err)
	}
	items := make([]StarredRepoTracker, 0, len(entities))
	for _, e := range entities {
		items = append(items, starredRepoTrackerFromEntity(e))
	}
	return items, PageResult{Page: f.Page, PerPage: f.PerPage, Total: total}, nil
}

func starredRepoTrackerFromEntity(e *entclient.StarredRepoTracker) StarredRepoTracker {
	return StarredRepoTracker{
		ID:                     e.ID,
		FullName:               e.FullName,
		State:                  e.State,
		ETag:                   e.Etag,
		LastReleaseID:          e.LastReleaseID,
		LastReleaseTag:         e.LastReleaseTag,
		LastReleasePublishedAt: e.LastReleasePublishedAt,
		NoReleaseSince:         e.NoReleaseSince,
		NoReleaseRecheckAt:     e.NoReleaseRecheckAt,
		FirstSeenAt:            e.FirstSeenAt,
		LastPollAt:             e.LastPollAt,
		CreatedAt:              e.CreatedAt,
		UpdatedAt:              e.UpdatedAt,
	}
}

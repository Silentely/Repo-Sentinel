package store_test

import (
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestStarredTrackerCRUD(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	tk := store.StarredRepoTracker{
		ID: "tk-1", FullName: "octocat/Hello-World", State: store.TrackerStateTracking,
		FirstSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := data.StarredTrackers().Upsert(ctx, tk); err != nil {
		t.Fatal(err)
	}
	got, err := data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if err != nil || got.ID != "tk-1" {
		t.Fatalf("get: %+v %v", got, err)
	}
	if _, err := data.StarredTrackers().GetByFullName(ctx, "nope/x"); err != store.ErrNotFound {
		t.Fatalf("未找到应返回 ErrNotFound, got %v", err)
	}
	if err := data.StarredTrackers().UpdateState(ctx, "tk-1", store.TrackerStateInactive); err != nil {
		t.Fatal(err)
	}
	if err := data.StarredTrackers().UpdatePollResult(ctx, "tk-1", `"etag1"`, 42, "v1.0", &now); err != nil {
		t.Fatal(err)
	}
	got, _ = data.StarredTrackers().GetByFullName(ctx, "octocat/Hello-World")
	if got.State != store.TrackerStateInactive || got.LastReleaseID != 42 || got.ETag != `"etag1"` || got.LastReleaseTag != "v1.0" {
		t.Fatalf("更新未生效: %+v", got)
	}
	cands, err := data.StarredTrackers().ListPollCandidates(ctx, 10)
	if err != nil || len(cands) != 0 {
		t.Fatalf("inactive 不应是轮询候选: %v %v", cands, err)
	}
	// 恢复 tracking 后应成为候选
	if err := data.StarredTrackers().UpdateState(ctx, "tk-1", store.TrackerStateTracking); err != nil {
		t.Fatal(err)
	}
	cands, _ = data.StarredTrackers().ListPollCandidates(ctx, 10)
	if len(cands) != 1 || cands[0].FullName != "octocat/Hello-World" {
		t.Fatalf("tracking 应为候选: %+v", cands)
	}
	// 计数与分页
	counts, err := data.StarredTrackers().CountByState(ctx)
	if err != nil || counts[store.TrackerStateTracking] != 1 {
		t.Fatalf("counts: %v %v", counts, err)
	}
	items, page, err := data.StarredTrackers().List(ctx, store.ListFilter{Page: 1, PerPage: 10, State: store.TrackerStateTracking})
	if err != nil || page.Total != 1 || len(items) != 1 {
		t.Fatalf("list: %v %v %v", items, page, err)
	}
}

func TestStarredTrackerUpsertIdempotent(t *testing.T) {
	data := openTestStore(t)
	ctx := t.Context()
	now := time.Now().UTC()
	tk := store.StarredRepoTracker{
		ID: "tk-2", FullName: "o/r", State: store.TrackerStateTracking,
		FirstSeenAt: now, CreatedAt: now, UpdatedAt: now,
	}
	if err := data.StarredTrackers().Upsert(ctx, tk); err != nil {
		t.Fatal(err)
	}
	tk2 := tk
	tk2.ETag = `"e2"`
	if err := data.StarredTrackers().Upsert(ctx, tk2); err != nil {
		t.Fatal(err)
	}
	items, page, err := data.StarredTrackers().List(ctx, store.ListFilter{Page: 1, PerPage: 10})
	if err != nil || page.Total != 1 {
		t.Fatalf("upsert 应幂等: %v %v %v", items, page, err)
	}
	got, _ := data.StarredTrackers().GetByFullName(ctx, "o/r")
	if got.ETag != `"e2"` {
		t.Fatalf("第二次 upsert 应更新 etag: %+v", got)
	}
}

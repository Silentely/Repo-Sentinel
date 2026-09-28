package syncx

import (
	"bytes"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// openReconcileProbeStore 打开隔离的 SQLite 库供写回语义探针使用。
func openReconcileProbeStore(t *testing.T) store.Store {
	t.Helper()
	data, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(t.TempDir(), "wb.db"),
	})
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = data.Close() })
	return data
}

// TestFinalizeSyncStatePreservesConcurrentArchive 回归：在途对账不得用开轮前的仓库快照
// 整体写回而抹平并发归档。原实现把候选读取时的 repo 快照直接交给 Upsert，而 Upsert 更新
// 分支对 SyncStatus/IsArchived 无条件覆写，导致 collapseArchived / repository.archived /
// 设置页写入的 archived 状态被还原为 active，留下「未归档 + active + 监控已关」的矛盾行，
// 且 GitHub 不再重发 repository.archived，该状态粘滞不自愈。
func TestFinalizeSyncStatePreservesConcurrentArchive(t *testing.T) {
	data := openReconcileProbeStore(t)
	ctx := t.Context()

	snapshot, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-wb-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "svc", FullName: "acme/svc", MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}

	// 并发归档收口：与 collapseArchived 完全相同的写入（IsArchived=true 会联动置
	// sync_status=archived 并关闭 monitor_enabled 与全部能力开关）。
	archived := true
	if err := data.Repositories().UpdateSettings(ctx, snapshot.ID, store.RepositorySettings{IsArchived: &archived}); err != nil {
		t.Fatalf("archive: %v", err)
	}

	// 用开轮前的旧快照走收尾写回。
	rec := &Reconciler{Store: data}
	rec.finalizeSyncState(ctx, snapshot, false, true, false)

	after, err := data.Repositories().Get(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if !after.IsArchived {
		t.Fatal("并发归档被抹平：is_archived 被还原为 false")
	}
	if after.SyncStatus != store.SyncStatusArchived {
		t.Fatalf("并发归档状态被抹平：sync_status=%q, want archived", after.SyncStatus)
	}
	if after.LastSyncedAt == nil {
		t.Fatal("本轮负责的同步进度字段仍应写回")
	}
}

// TestFinalizeSyncStateDoesNotResurrectDeletedRepo 回归：并发彻底删除不得被在途对账的
// 写回撤销。原实现用旧快照调 Upsert，而 Upsert 按 full_name 查不到行时走新建分支，
// 以同一 ID 重建该行且不设置 monitor_enabled（schema 默认 true），仓库随即重新成为
// 同步候选并恢复活跃轮询。
func TestFinalizeSyncStateDoesNotResurrectDeletedRepo(t *testing.T) {
	data := openReconcileProbeStore(t)
	ctx := t.Context()

	snapshot, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-wb-2", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "gone", FullName: "acme/gone", MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	if err := data.Repositories().DeleteRepository(ctx, snapshot.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}

	rec := &Reconciler{Store: data}
	rec.finalizeSyncState(ctx, snapshot, false, true, false)

	if _, err := data.Repositories().Get(ctx, snapshot.ID); err == nil {
		t.Fatal("已彻底删除的仓库被在途对账写回复活")
	}
	// 也不得按 full_name 重建。
	if _, err := data.Repositories().GetByFullName(ctx, "acme/gone"); err == nil {
		t.Fatal("已删除仓库按 full_name 被重建")
	}
}

// TestFinalizeSyncStateAdvancesBaselineOnlyFromBaseline 基线放行只在本轮起始状态仍是
// baseline 时生效：当前行已被并发推进到其它状态时不得回退。
func TestFinalizeSyncStateAdvancesBaselineOnlyFromBaseline(t *testing.T) {
	data := openReconcileProbeStore(t)
	ctx := t.Context()

	snapshot, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-wb-3", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusBaseline,
		Owner: "acme", Name: "base", FullName: "acme/base", MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 并发把状态改为 unavailable（如 GitHub 侧 404 收口）。
	if err := data.Repositories().UpdateSyncStatus(ctx, snapshot.ID, store.SyncStatusUnavailable); err != nil {
		t.Fatalf("status: %v", err)
	}

	rec := &Reconciler{Store: data}
	rec.finalizeSyncState(ctx, snapshot, true, true, false)

	after, err := data.Repositories().Get(ctx, snapshot.ID)
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if after.SyncStatus != store.SyncStatusUnavailable {
		t.Fatalf("并发写入的状态被回退：sync_status=%q, want unavailable", after.SyncStatus)
	}
}

// TestFinalizeSyncStateLogsReloadFailure 重读失败（非 NotFound）时必须留痕而非静默跳过。
func TestFinalizeSyncStateLogsReloadFailure(t *testing.T) {
	var logs bytes.Buffer
	data := openReconcileProbeStore(t)
	ctx := t.Context()

	snapshot, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: "repo-wb-4", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "log", FullName: "acme/log", MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("seed: %v", err)
	}
	// 关库使随后的 Get 失败（既非成功也非 NotFound）。
	if err := data.Close(); err != nil {
		t.Fatalf("close: %v", err)
	}

	rec := &Reconciler{Store: data, Logger: slog.New(slog.NewJSONHandler(&logs, nil))}
	rec.finalizeSyncState(ctx, snapshot, false, true, false)

	if got := logs.String(); got == "" {
		t.Fatal("重读失败应 Warn 留痕")
	} else if !bytes.Contains(logs.Bytes(), []byte("repo_reload_failed")) {
		t.Fatalf("留痕应含 repo_reload_failed，实际：%s", got)
	}
}

package syncx

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/digest"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestSchedulerFailureLogIncludesTaskAndDuration(t *testing.T) {
	var logBuffer bytes.Buffer
	s := &Scheduler{Logger: slog.New(slog.NewJSONHandler(&logBuffer, nil))}

	s.runScheduledTask(t.Context(), "digest", "scheduled digest failed", "digest_failed", func(_ context.Context) error {
		return errors.New("digest boom")
	})

	logs := logBuffer.String()
	for _, want := range []string{`"task":"digest"`, `"duration_ms":`, `"error_code":"digest_failed"`, `"error":"digest boom"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("调度失败日志应包含 %s，实际: %s", want, logs)
		}
	}
}

// TestSchedulerSuccessLogsAtDebug 成功留痕必须是 Debug 级：默认 Info 下不刷屏，
// 开 debug 排查时能确认「任务已执行」并看到耗时。
func TestSchedulerSuccessLogsAtDebug(t *testing.T) {
	var logBuffer bytes.Buffer
	s := &Scheduler{Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug}))}

	s.runScheduledTask(t.Context(), "external_poll", "scheduled external poll failed", "external_poll_failed", func(_ context.Context) error {
		return nil
	})

	logs := logBuffer.String()
	if !strings.Contains(logs, `"level":"DEBUG"`) || !strings.Contains(logs, `"msg":"scheduled task ok"`) {
		t.Fatalf("成功留痕应为 DEBUG 级 scheduled task ok，实际: %s", logs)
	}
	if !strings.Contains(logs, `"task":"external_poll"`) || !strings.Contains(logs, `"duration_ms":`) {
		t.Fatalf("成功留痕应携带 task 与 duration_ms，实际: %s", logs)
	}
}

// TestSchedulerStarredTasksLogged 验证 star 同步与 release 轮询经调度留痕。
func TestSchedulerStarredTasksLogged(t *testing.T) {
	var logBuffer bytes.Buffer
	// Store 为 nil 时 Poller 快速跳过，仅验证调度包装与留痕。
	s := &Scheduler{
		Starred: &StarredReleasePoller{},
		Logger:  slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
	}
	s.runStarred(context.Background())
	logs := logBuffer.String()
	for _, want := range []string{`"task":"star_sync"`, `"task":"release_poll"`, `"level":"DEBUG"`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("starred 调度留痕应包含 %s，实际: %s", want, logs)
		}
	}
}

// TestSchedulerStarredNilSafe 验证 Starred 未装配时不 panic。
func TestSchedulerStarredNilSafe(t *testing.T) {
	s := &Scheduler{}
	s.runStarred(context.Background()) // 不应 panic
}

// upsertSetting 写入系统设置（digest.Generator 的运行参数载体）。
func upsertSetting(t *testing.T, data store.Store, key string, value any) {
	t.Helper()
	raw, err := json.Marshal(value)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := data.Settings().Upsert(t.Context(), store.SystemSetting{
		ID: ulid.Make().String(), Key: key, ValueJSON: raw,
		UpdatedAt: time.Now().UTC(), UpdatedBy: "test",
	}); err != nil {
		t.Fatal(err)
	}
}

// TestSchedulerTicksAllComponentsAndStops 验证三路 ticker 各调度至少一次，且 ctx 取消后 Run 返回。
// Scheduler 的周期是结构体导出字段（ReconcileEvery/ExternalEvery/DigestEvery），可直接注入毫秒级周期。
func TestSchedulerTicksAllComponentsAndStops(t *testing.T) {
	data := openSyncStore(t)
	ctx := t.Context()

	// 对账调度观测：OnRun 在每轮 ReconcileAll 入口必触发，无需真实 GitHub。
	var reconcileRuns atomic.Int64
	reconciler := &Reconciler{Store: data, GitHub: nil, OnRun: func() { reconcileRuns.Add(1) }}

	// 外部轮询观测：注册一个 external 仓，每轮 PollAll 会命中伪公开 API，请求计数即调度证据。
	var externalPolls atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		externalPolls.Add(1)
		_ = json.NewEncoder(w).Encode([]any{})
	}))
	t.Cleanup(srv.Close)
	if _, err := data.Repositories().Upsert(ctx, store.Repository{
		ID: ulid.Make().String(), Type: store.RepositoryTypeExternal, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "ext", FullName: "acme/ext",
	}); err != nil {
		t.Fatal(err)
	}
	external := &ExternalPoller{Store: data, Client: &githubx.PublicClient{BaseURL: srv.URL, HTTP: srv.Client()}}

	// 摘要调度观测：Generator 无回调注入点，借助其既有副作用——
	// 把发送时刻配为当前 UTC 小时并允许空摘要，RunOnce 落写 digest.last_sent_date 即证明被调度。
	upsertSetting(t, data, "digest.local_time", time.Now().UTC().Format("15")+":00")
	upsertSetting(t, data, "digest.send_empty", true)
	digestGen := &digest.Generator{Store: data}

	s := &Scheduler{
		Reconciler: reconciler, External: external, Digest: digestGen,
		ReconcileEvery: 20 * time.Millisecond,
		ExternalEvery:  20 * time.Millisecond,
		DigestEvery:    20 * time.Millisecond,
	}
	// Run 启动后主动补一次对账+轮询的 startup 定时器为 45s，本用例只覆盖 ticker 路径。
	runCtx, cancel := context.WithCancel(ctx)
	done := make(chan struct{})
	go func() {
		s.Run(runCtx)
		close(done)
	}()

	// 轮询等待三路各自触发（上限 3s，容忍 CI 慢机调度抖动）。
	deadline := time.Now().Add(3 * time.Second)
	for {
		digestRan := false
		if _, err := data.Settings().Get(ctx, "digest.last_sent_date"); err == nil {
			digestRan = true
		}
		if reconcileRuns.Load() >= 1 && externalPolls.Load() >= 1 && digestRan {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("三路组件未全部调度：reconcile=%d external=%d digest=%v",
				reconcileRuns.Load(), externalPolls.Load(), digestRan)
		}
		time.Sleep(10 * time.Millisecond)
	}

	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ctx 取消后 Run 未在 2s 内返回")
	}
	// 三路均已确认至少一次，复核最终计数作为回归记录。
	if reconcileRuns.Load() < 1 || externalPolls.Load() < 1 {
		t.Fatalf("调度计数异常：reconcile=%d external=%d", reconcileRuns.Load(), externalPolls.Load())
	}
}

package webhooksvc_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
)

// openServiceStore 打开隔离的 SQLite 测试库（与 webhook_handlers_test 同源约束：
// Atlas 使用 sql.DB 时会取 TMPDIR 下的固定锁名，每个测试必须独立目录）。
func openServiceStore(t *testing.T) store.Store {
	t.Helper()
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver: "sqlite",
		URL:    "file:" + filepath.Join(temporaryDir, "webhooksvc.db"),
	})
	if err != nil {
		t.Fatalf("打开 webhooksvc 测试 Store 失败: %v", err)
	}
	t.Cleanup(func() { _ = opened.Close() })
	return opened
}

// seedActiveDemoRepo 预置 acme/demo 为 active 状态：通知管线中活跃仓库不抑制实时通知，
// 才能触发 Evaluator 被调用（基线仓库会走 SuppressNotify 早退）。
func seedActiveDemoRepo(t *testing.T, data store.Store) store.Repository {
	t.Helper()
	repo, err := data.Repositories().Upsert(t.Context(), store.Repository{
		ID: "repo-demo", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "demo", FullName: "acme/demo",
		HTMLURL: "https://github.com/acme/demo",
	})
	if err != nil {
		t.Fatalf("seed repo: %v", err)
	}
	return repo
}

// issueOpenedPayload 构造 GitHub issues「opened」事件载荷。
func issueOpenedPayload(t *testing.T) []byte {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number":     7,
			"title":      "hello",
			"state":      "open",
			"html_url":   "https://github.com/acme/demo/issues/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": time.Now().UTC().Format(time.RFC3339),
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": map[string]any{
			"id":             99,
			"name":           "demo",
			"full_name":      "acme/demo",
			"private":        false,
			"html_url":       "https://github.com/acme/demo",
			"default_branch": "main",
			"owner":          map[string]any{"login": "acme"},
		},
	})
	if err != nil {
		t.Fatalf("构造 issue payload 失败: %v", err)
	}
	return payload
}

// recordingEvaluator 记录调用次数与仓库名，便于断言 Evaluator 是否被触发。
type recordingEvaluator struct {
	called   int
	lastRepo string
}

func (r *recordingEvaluator) Evaluate(_ context.Context, _ normalizer.Result, repoFullName string) error {
	r.called++
	r.lastRepo = repoFullName
	return nil
}

// failingEvaluator 总是返回错误，用于验证 rule 失败路径。
type failingEvaluator struct{}

func (failingEvaluator) Evaluate(context.Context, normalizer.Result, string) error {
	return errors.New("rule engine exploded")
}

func newService(data store.Store, evaluator webhooksvc.Evaluator, bg context.Context) *webhooksvc.Service {
	return &webhooksvc.Service{
		Store:      data,
		Logger:     slog.New(slog.NewJSONHandler(io.Discard, nil)),
		Evaluator:  evaluator,
		Background: bg,
	}
}

// seedDelivery 创建一条 accepted 状态的行并返回其 ID（rowID 是 MarkProcessed 的目标）。
func seedDelivery(t *testing.T, data store.Store, deliveryID, eventType string, payload []byte) string {
	t.Helper()
	row, err := data.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		DeliveryID: deliveryID,
		EventType:  eventType,
		Payload:    payload,
	})
	if err != nil {
		t.Fatalf("创建 WebhookDelivery 失败: %v", err)
	}
	return row.ID
}

// mustGetDelivery 读取 delivery 记录，失败即终止。
func mustGetDelivery(t *testing.T, data store.Store, deliveryID string) store.WebhookDelivery {
	t.Helper()
	row, err := data.WebhookDeliveries().GetByDeliveryID(t.Context(), deliveryID)
	if err != nil {
		t.Fatalf("读取 delivery %s 失败: %v", deliveryID, err)
	}
	return row
}

// TestProcessNormalizeFailureMarksFailed 非法载荷 → normalize 失败 → 行标记 failed。
func TestProcessNormalizeFailureMarksFailed(t *testing.T) {
	data := openServiceStore(t)
	svc := newService(data, nil, t.Context())
	rowID := seedDelivery(t, data, "delivery-bad", "issues", []byte(`{not json`))

	svc.Process(rowID, "issues", "delivery-bad", []byte(`{not json`))

	got := mustGetDelivery(t, data, "delivery-bad")
	if got.Status != store.DeliveryFailed || got.ErrorCode != "normalize_failed" {
		t.Fatalf("normalize 失败应标记 failed/normalize_failed，got status=%s code=%s", got.Status, got.ErrorCode)
	}
}

// TestProcessIssueOpenedMarksProcessed 合法载荷 + 活跃仓库 → Evaluator 被调用 → processed。
func TestProcessIssueOpenedMarksProcessed(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	evaluator := &recordingEvaluator{}
	svc := newService(data, evaluator, t.Context())
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-ok", "issues", payload)

	svc.Process(rowID, "issues", "delivery-ok", payload)

	got := mustGetDelivery(t, data, "delivery-ok")
	if got.Status != store.DeliveryProcessed {
		t.Fatalf("成功处理应标记 processed，got status=%s code=%s", got.Status, got.ErrorCode)
	}
	if evaluator.called != 1 || evaluator.lastRepo != "acme/demo" {
		t.Fatalf("Evaluator 应被调用 1 次且仓库为 acme/demo，got called=%d repo=%q", evaluator.called, evaluator.lastRepo)
	}
}

// TestProcessEvaluatorErrorMarksFailed Evaluator 失败 → 行标记 failed/rule_failed。
func TestProcessEvaluatorErrorMarksFailed(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	svc := newService(data, failingEvaluator{}, t.Context())
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-rule", "issues", payload)

	svc.Process(rowID, "issues", "delivery-rule", payload)

	got := mustGetDelivery(t, data, "delivery-rule")
	if got.Status != store.DeliveryFailed || got.ErrorCode != "rule_failed" {
		t.Fatalf("rule 失败应标记 failed/rule_failed，got status=%s code=%s", got.Status, got.ErrorCode)
	}
}

// TestProcessRuleErrorLogCarriesRepo rule 失败日志必须携带仓库名，
// 便于按仓库聚合定位通知链路故障（错误行只有 rowID 时难以排查）。
func TestProcessRuleErrorLogCarriesRepo(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	var logBuffer bytes.Buffer
	svc := &webhooksvc.Service{
		Store:      data,
		Logger:     slog.New(slog.NewJSONHandler(&logBuffer, nil)),
		Evaluator:  failingEvaluator{},
		Background: t.Context(),
	}
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-rule-log", "issues", payload)
	svc.Process(rowID, "issues", "delivery-rule-log", payload)

	logs := logBuffer.String()
	if !strings.Contains(logs, "rule evaluate failed") {
		t.Fatalf("应记录 rule evaluate failed，实际日志: %s", logs)
	}
	if !strings.Contains(logs, "acme/demo") {
		t.Fatalf("rule 失败日志应携带仓库名 acme/demo，实际日志: %s", logs)
	}
}

// TestProcessFailureInvokesOnFailed 规范化与规则评估失败都必须触发 OnFailed 指标回调，
// 保证 httpapi 的 webhook_failed_total 与实际失败行一致。
func TestProcessFailureInvokesOnFailed(t *testing.T) {
	t.Run("normalize_failed", func(t *testing.T) {
		data := openServiceStore(t)
		seedActiveDemoRepo(t, data)
		called := 0
		svc := &webhooksvc.Service{
			Store:      data,
			Evaluator:  failingEvaluator{},
			Background: t.Context(),
			OnFailed:   func() { called++ },
		}
		// 非法 JSON 触发规范化失败。
		bad := []byte(`{not-json`)
		rowID := seedDelivery(t, data, "delivery-bad", "issues", bad)

		svc.Process(rowID, "issues", "delivery-bad", bad)

		if called != 1 {
			t.Fatalf("规范化失败应触发 OnFailed 一次，实际 %d", called)
		}
	})

	t.Run("rule_failed", func(t *testing.T) {
		data := openServiceStore(t)
		seedActiveDemoRepo(t, data)
		called := 0
		svc := &webhooksvc.Service{
			Store:      data,
			Evaluator:  failingEvaluator{},
			Background: t.Context(),
			OnFailed:   func() { called++ },
		}
		payload := issueOpenedPayload(t)
		rowID := seedDelivery(t, data, "delivery-rule-cb", "issues", payload)

		svc.Process(rowID, "issues", "delivery-rule-cb", payload)

		if called != 1 {
			t.Fatalf("规则评估失败应触发 OnFailed 一次，实际 %d", called)
		}
	})
}

// TestProcessSuccessLogCarriesStaleDiscarded 乱序丢弃是「处理了但没通知」的常见原因，
// 成功日志必须带 stale_discarded 布尔，避免排障时把正常入库误判为通知丢失。
func TestProcessSuccessLogCarriesStaleDiscarded(t *testing.T) {
	data := openServiceStore(t)
	repo := seedActiveDemoRepo(t, data)
	var logBuffer bytes.Buffer
	// 成功日志已降为 Debug 级：用 LevelDebug 捕获，守护字段不丢。
	svc := &webhooksvc.Service{
		Store: data,
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Background: t.Context(),
	}
	// 预置较新的 issue：之后到达的旧载荷会被 UpsertIfNewer 判定乱序丢弃。
	if _, _, err := data.WorkItems().UpsertIfNewer(t.Context(), store.WorkItem{
		ID: "wi-stale", RepositoryID: repo.ID, Number: 7, Kind: store.WorkItemKindIssue,
		State: "closed", Title: "最新状态", Author: "alice",
		HTMLURL:         "https://github.com/acme/demo/issues/7",
		SourceUpdatedAt: time.Now().UTC().Add(time.Hour), StateHash: "h-new",
	}); err != nil {
		t.Fatal(err)
	}
	// 旧时间的 opened 载荷（updated_at 远早于库内状态）。
	old := time.Now().UTC().Add(-48 * time.Hour)
	payload, err := json.Marshal(map[string]any{
		"action": "opened",
		"issue": map[string]any{
			"number": 7, "title": "过期打开", "state": "open",
			"html_url":   "https://github.com/acme/demo/issues/7",
			"user":       map[string]any{"login": "alice"},
			"updated_at": old.Format(time.RFC3339),
			"labels":     []any{},
			"assignees":  []any{},
		},
		"repository": map[string]any{
			"id": 99, "name": "demo", "full_name": "acme/demo", "private": false,
			"html_url": "https://github.com/acme/demo", "default_branch": "main",
			"owner": map[string]any{"login": "acme"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	rowID := seedDelivery(t, data, "delivery-stale", "issues", payload)

	svc.Process(rowID, "issues", "delivery-stale", payload)

	logs := logBuffer.String()
	if !strings.Contains(logs, `"msg":"github webhook processed"`) {
		t.Fatalf("应记录成功日志，实际: %s", logs)
	}
	if !strings.Contains(logs, `"stale_discarded":true`) {
		t.Fatalf("乱序丢弃应在成功日志带 stale_discarded=true，实际: %s", logs)
	}
}

// TestProcessSlowLogsWarn 处理耗时超过阈值时必须 Warn 留痕（含 delivery/event/repo/duration）：
// 数据库抖动或外部调用阻塞会拖慢 webhook 管线，慢日志便于定位。
func TestProcessSlowLogsWarn(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	var logBuffer bytes.Buffer
	svc := &webhooksvc.Service{
		Store:         data,
		Logger:        slog.New(slog.NewJSONHandler(&logBuffer, nil)),
		Evaluator:     slowEvaluator{delay: 5 * time.Millisecond},
		Background:    t.Context(),
		SlowThreshold: time.Millisecond,
	}
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-slow", "issues", payload)

	svc.Process(rowID, "issues", "delivery-slow", payload)

	logs := logBuffer.String()
	for _, want := range []string{`"msg":"webhook process slow"`, `"delivery_id":"delivery-slow"`, `"repo":"acme/demo"`, `"error_code":"webhook_slow"`, `"duration_ms":`} {
		if !strings.Contains(logs, want) {
			t.Fatalf("慢处理日志应包含 %s，实际: %s", want, logs)
		}
	}
}

// slowEvaluator 延迟指定时长后成功返回，用于稳定触发慢处理阈值。
type slowEvaluator struct {
	delay time.Duration
}

func (e slowEvaluator) Evaluate(ctx context.Context, _ normalizer.Result, _ string) error {
	select {
	case <-time.After(e.delay):
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// TestProcessSuccessLogCarriesEventID 成功日志必须带 event_id：delivery 行 ↔ 事件
// 可互相检索定位，排查"这条投递对应哪个事件"不需要二次查询。
func TestProcessSuccessLogCarriesEventID(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	var logBuffer bytes.Buffer
	// 成功日志已降为 Debug 级：用 LevelDebug 捕获，守护字段不丢。
	svc := &webhooksvc.Service{
		Store: data,
		Logger: slog.New(slog.NewJSONHandler(&logBuffer, &slog.HandlerOptions{Level: slog.LevelDebug})),
		Background: t.Context(),
	}
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-event-id", "issues", payload)

	svc.Process(rowID, "issues", "delivery-event-id", payload)

	logs := logBuffer.String()
	if !strings.Contains(logs, `"event_kind":"issue"`) || !strings.Contains(logs, `"event_id":"`) {
		t.Fatalf("成功日志应带 event_kind 与 event_id，实际: %s", logs)
	}
}

// TestProcessBaselineSuppressSkipsEvaluator 新仓库（基线）→ 抑制实时通知，Evaluator 不触发，
// 但行仍标记 processed（规范化本身成功）。
func TestProcessBaselineSuppressSkipsEvaluator(t *testing.T) {
	data := openServiceStore(t)
	evaluator := &recordingEvaluator{}
	svc := newService(data, evaluator, t.Context())
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-baseline", "issues", payload)

	svc.Process(rowID, "issues", "delivery-baseline", payload)

	got := mustGetDelivery(t, data, "delivery-baseline")
	if got.Status != store.DeliveryProcessed {
		t.Fatalf("基线抑制也应标记 processed，got status=%s code=%s", got.Status, got.ErrorCode)
	}
	if evaluator.called != 0 {
		t.Fatalf("基线仓库不应触发 Evaluator，got called=%d", evaluator.called)
	}
}

// TestProcessNilBackgroundReturns 未注入 Background 时 Process 直接返回，
// 行保持 accepted（调用方负责重试或忽略），且不 panic。
func TestProcessNilBackgroundReturns(t *testing.T) {
	data := openServiceStore(t)
	svc := newService(data, nil, nil)
	payload := issueOpenedPayload(t)
	rowID := seedDelivery(t, data, "delivery-nobg", "issues", payload)

	svc.Process(rowID, "issues", "delivery-nobg", payload)

	got := mustGetDelivery(t, data, "delivery-nobg")
	if got.Status != store.DeliveryAccepted {
		t.Fatalf("Background 为 nil 时行应保持 accepted，got status=%s", got.Status)
	}
}

// TestProcessDuplicateFingerprintNoPanic 同一载荷重复投递（指纹命中）应正常返回并标记 processed。
func TestProcessDuplicateFingerprintNoPanic(t *testing.T) {
	data := openServiceStore(t)
	seedActiveDemoRepo(t, data)
	svc := newService(data, &recordingEvaluator{}, t.Context())
	payload := issueOpenedPayload(t)

	firstID := seedDelivery(t, data, "delivery-first", "issues", payload)
	svc.Process(firstID, "issues", "delivery-first", payload)

	secondID := seedDelivery(t, data, "delivery-second", "issues", payload)
	svc.Process(secondID, "issues", "delivery-second", payload)

	if got := mustGetDelivery(t, data, "delivery-second"); got.Status != store.DeliveryProcessed {
		t.Fatalf("重复载荷第二次处理应 processed，got status=%s code=%s", got.Status, got.ErrorCode)
	}
}

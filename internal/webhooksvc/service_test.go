package webhooksvc_test

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"path/filepath"
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

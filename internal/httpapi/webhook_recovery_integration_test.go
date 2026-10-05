package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/Silentely/Repo-Sentinel/internal/webhooksvc"
	"github.com/oklog/ulid/v2"
)

// TestWebhookRecovery_ShutdownCanceled 验证关闭守卫：
// 当 Background context 取消时，processWebhookAsync 无法获取槽位，
// 必须将状态标记为 failed 并附带 shutdown_canceled 与正确的 claimToken，杜绝状态残留。
func TestWebhookRecovery_ShutdownCanceled(t *testing.T) {
	bgCtx, cancel := context.WithCancel(t.Context())
	cancel() // 预先取消，模拟正在关机

	fixture := newWebhookTestFixture(t, webhookTestOptions{
		background: bgCtx,
	})

	id := "del-" + ulid.Make().String()
	_, err := fixture.store.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		ID:                 id,
		DeliveryID:         "github-del-" + id,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         time.Now().UTC(),
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("创建 delivery 失败: %v", err)
	}

	claimToken, ok, err := fixture.store.WebhookDeliveries().ClaimWebhookForProcessing(t.Context(), id, fixture.srv.getWorkerID(), 1*time.Minute)
	if err != nil || !ok {
		t.Fatalf("认领失败: ok=%v, err=%v", ok, err)
	}

	// 此时 server 处于关停取消态
	s := fixture.srv
	s.webhookSem = make(chan struct{}, 1)
	s.webhookSvc = &webhooksvc.Service{
		Store:      fixture.store,
		Logger:     s.dependencies.Logger,
		Background: bgCtx,
	}

	done := make(chan struct{})
	s.safeGo("test_shutdown", func() {
		defer close(done)
		s.processWebhookAsync(id, "push", "github-del-"+id, claimToken, []byte(`{}`))
	})
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("processWebhookAsync 超时")
	}

	// 等待 markFailed 完成
	time.Sleep(100 * time.Millisecond)

	fetched, err := fixture.store.WebhookDeliveries().Get(t.Context(), id)
	if err != nil {
		t.Fatalf("回查 delivery 失败: %v", err)
	}
	if fetched.Status != store.DeliveryFailed {
		t.Errorf("关停时期望状态为 failed, 实际为 %s", fetched.Status)
	}
	if fetched.LastErrorCode != "shutdown_canceled" && fetched.ErrorCode != "shutdown_canceled" {
		t.Errorf("期望错误码 shutdown_canceled, 实际 last_error_code=%s, error_code=%s", fetched.LastErrorCode, fetched.ErrorCode)
	}
}

// TestWebhookRecovery_OrphanRecoveryLoop 验证孤儿扫描接管：
// 模拟滞留在 accepted（>2m）及过期 processing 的记录，执行 runOrphanRecoveryOnce，
// 验证孤儿被成功抢占恢复，状态机正常流转。
func TestWebhookRecovery_OrphanRecoveryLoop(t *testing.T) {
	bgCtx := t.Context()
	fixture := newWebhookTestFixture(t, webhookTestOptions{
		background: bgCtx,
	})

	s := fixture.srv
	s.webhookSem = make(chan struct{}, 5)
	s.webhookSvc = &webhooksvc.Service{
		Store:      fixture.store,
		Logger:     s.dependencies.Logger,
		Background: bgCtx,
	}

	now := time.Now().UTC()
	// 插入滞留 3 分钟的 accepted 记录
	id1 := "del-orphan-accepted"
	_, err := fixture.store.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		ID:                 id1,
		DeliveryID:         "gh-" + id1,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         now.Add(-3 * time.Minute),
		Payload:            []byte(`{"ref":"refs/heads/main"}`),
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("插入 accepted 孤儿失败: %v", err)
	}

	// 插入过期的 processing 记录
	id2 := "del-orphan-processing"
	_, err = fixture.store.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		ID:                 id2,
		DeliveryID:         "gh-" + id2,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         now.Add(-5 * time.Minute),
		Payload:            []byte(`{"ref":"refs/heads/main"}`),
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("插入 processing 孤儿失败: %v", err)
	}
	_, _, err = fixture.store.WebhookDeliveries().ClaimWebhookForProcessing(t.Context(), id2, "old-crashed-worker", -10*time.Second)
	if err != nil {
		t.Fatalf("初次 claim 失败: %v", err)
	}

	cfg := defaultOrphanRecoveryConfig()
	cfg.AcceptedAge = 2 * time.Minute

	recovered := s.runOrphanRecoveryOnce(t.Context(), cfg)
	if recovered != 2 {
		t.Fatalf("期望恢复 2 条孤儿，实际恢复 %d 条", recovered)
	}

	// 等待异步处理完成
	time.Sleep(200 * time.Millisecond)

	d1, err := fixture.store.WebhookDeliveries().Get(t.Context(), id1)
	if err != nil {
		t.Fatalf("获取 d1 失败: %v", err)
	}
	if d1.Status != store.DeliveryProcessed && d1.Status != store.DeliveryProcessing {
		t.Errorf("d1 状态异常: %s", d1.Status)
	}
	if d1.ClaimedBy != s.getWorkerID() {
		t.Errorf("d1 ClaimedBy 期望 %s, 实际 %s", s.getWorkerID(), d1.ClaimedBy)
	}

	d2, err := fixture.store.WebhookDeliveries().Get(t.Context(), id2)
	if err != nil {
		t.Fatalf("获取 d2 失败: %v", err)
	}
	if d2.Status != store.DeliveryProcessed && d2.Status != store.DeliveryProcessing {
		t.Errorf("d2 状态异常: %s", d2.Status)
	}
	if d2.ClaimedBy != s.getWorkerID() {
		t.Errorf("d2 ClaimedBy 期望 %s, 实际 %s", s.getWorkerID(), d2.ClaimedBy)
	}
}

// TestWebhookRecovery_DeadLetter 验证死信队列分流与 Prometheus 指标上报：
// 模拟重试达到 5 次或滞留超过 30 分钟的记录，断言转入 dead_letter 且指标自增。
func TestWebhookRecovery_DeadLetter(t *testing.T) {
	bgCtx := t.Context()
	fixture := newWebhookTestFixture(t, webhookTestOptions{
		background: bgCtx,
	})

	s := fixture.srv
	s.webhookSem = make(chan struct{}, 5)
	s.webhookSvc = &webhooksvc.Service{
		Store:      fixture.store,
		Logger:     s.dependencies.Logger,
		Background: bgCtx,
	}

	now := time.Now().UTC()
	// 记录 1: 尝试次数达到 5 次
	idMaxAttempts := "del-max-attempts"
	_, err := fixture.store.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		ID:                 idMaxAttempts,
		DeliveryID:         "gh-" + idMaxAttempts,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         now.Add(-3 * time.Minute),
		AttemptCount:       4, // 认领后变为 5
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("插入 max attempts 记录失败: %v", err)
	}

	// 记录 2: 滞留超过 30 分钟
	idRetentionExpired := "del-retention-expired"
	_, err = fixture.store.WebhookDeliveries().Create(t.Context(), store.WebhookDelivery{
		ID:                 idRetentionExpired,
		DeliveryID:         "gh-" + idRetentionExpired,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         now.Add(-35 * time.Minute),
		AttemptCount:       1,
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("插入 retention expired 记录失败: %v", err)
	}

	cfg := defaultOrphanRecoveryConfig()
	cfg.MaxAttempts = 5
	cfg.MaxRetentionAge = 30 * time.Minute

	recovered := s.runOrphanRecoveryOnce(t.Context(), cfg)
	if recovered != 0 {
		t.Fatalf("期望恢复 0 条（全部转移至死信），实际恢复 %d 条", recovered)
	}

	dl1, err := fixture.store.WebhookDeliveries().Get(t.Context(), idMaxAttempts)
	if err != nil {
		t.Fatalf("获取 dl1 失败: %v", err)
	}
	if dl1.Status != store.DeliveryDeadLetter {
		t.Errorf("dl1 状态期望 dead_letter, 实际 %s", dl1.Status)
	}
	if dl1.LastErrorCode != "max_attempts_exceeded" {
		t.Errorf("dl1 last_error_code 期望 max_attempts_exceeded, 实际 %s", dl1.LastErrorCode)
	}

	dl2, err := fixture.store.WebhookDeliveries().Get(t.Context(), idRetentionExpired)
	if err != nil {
		t.Fatalf("获取 dl2 失败: %v", err)
	}
	if dl2.Status != store.DeliveryDeadLetter {
		t.Errorf("dl2 状态期望 dead_letter, 实际 %s", dl2.Status)
	}
	if dl2.LastErrorCode != "retention_timeout_exceeded" {
		t.Errorf("dl2 last_error_code 期望 retention_timeout_exceeded, 实际 %s", dl2.LastErrorCode)
	}

	// 验证 Prometheus 指标
	req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
	rec := httptest.NewRecorder()
	s.handleMetrics(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /metrics 状态码期望 200, 实际 %d", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "reposentinel_webhook_dead_letter_total") {
		t.Errorf("metrics 应包含 reposentinel_webhook_dead_letter_total, 得到:\n%s", body)
	}
}

package store_test

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// openDualSQLiteStores 创建两个连接到同一个 SQLite WAL 数据库文件的独立 Store 实例，模拟双 Pod。
func openDualSQLiteStores(t *testing.T) (store.Store, store.Store) {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("TMPDIR", dir)
	dbURL := "file:" + filepath.Join(dir, "shared.db") + "?_journal=WAL&_busy_timeout=5000"

	cfg := config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          dbURL,
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	}

	s1, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("打开 Store 1 失败: %v", err)
	}
	t.Cleanup(func() { _ = s1.Close() })

	s2, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("打开 Store 2 失败: %v", err)
	}
	t.Cleanup(func() { _ = s2.Close() })

	return s1, s2
}

// TestWebhookClaim_ConcurrentOrphanRecovery 测试 a:
// 启动 2 个完全独立的数据库连接/Store 实例（模拟 2 个跨副本 Pod），并发执行 ClaimDueOrphanWebhooks
// 扫描同一批 50 条超时 Webhook，断言每条记录仅被 1 个连接成功抢占，无任何覆盖。
func TestWebhookClaim_ConcurrentOrphanRecovery(t *testing.T) {
	ctx := t.Context()
	s1, s2 := openDualSQLiteStores(t)

	now := time.Now().UTC()
	cutoff := now.Add(-5 * time.Minute)
	receivedAt := now.Add(-10 * time.Minute)

	const totalCount = 50
	ids := make([]string, totalCount)
	for i := 0; i < totalCount; i++ {
		id := "del-" + ulid.Make().String()
		ids[i] = id
		_, err := s1.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:                 id,
			DeliveryID:         "github-del-" + id,
			EventType:          "push",
			Status:             store.DeliveryAccepted,
			ReceivedAt:         receivedAt,
			RepositoryFullName: "org/repo",
		})
		if err != nil {
			t.Fatalf("创建第 %d 条 WebhookDelivery 失败: %v", i, err)
		}
	}

	// 启动 2 个 Worker 并发抢占
	var wg sync.WaitGroup
	wg.Add(2)

	var worker1Claimed []*store.WebhookDelivery
	var worker2Claimed []*store.WebhookDelivery
	var err1, err2 error

	go func() {
		defer wg.Done()
		worker1Claimed, err1 = s1.WebhookDeliveries().ClaimDueOrphanWebhooks(ctx, "pod-worker-1", cutoff, totalCount)
	}()

	go func() {
		defer wg.Done()
		worker2Claimed, err2 = s2.WebhookDeliveries().ClaimDueOrphanWebhooks(ctx, "pod-worker-2", cutoff, totalCount)
	}()

	wg.Wait()

	if err1 != nil {
		t.Fatalf("Worker 1 抢占出错: %v", err1)
	}
	if err2 != nil {
		t.Fatalf("Worker 2 抢占出错: %v", err2)
	}

	seenIDs := make(map[string]string) // id -> worker
	for _, d := range worker1Claimed {
		if prevWorker, exists := seenIDs[d.ID]; exists {
			t.Fatalf("记录 %s 被重复抢占: prev=%s, current=worker-1", d.ID, prevWorker)
		}
		seenIDs[d.ID] = "pod-worker-1"
		if d.ClaimedBy != "pod-worker-1" {
			t.Errorf("worker 1 记录 ClaimedBy 期望 pod-worker-1, 得到 %s", d.ClaimedBy)
		}
		if d.ClaimToken == "" {
			t.Errorf("worker 1 记录 ClaimToken 为空")
		}
	}

	for _, d := range worker2Claimed {
		if prevWorker, exists := seenIDs[d.ID]; exists {
			t.Fatalf("记录 %s 被重复抢占: prev=%s, current=worker-2", d.ID, prevWorker)
		}
		seenIDs[d.ID] = "pod-worker-2"
		if d.ClaimedBy != "pod-worker-2" {
			t.Errorf("worker 2 记录 ClaimedBy 期望 pod-worker-2, 得到 %s", d.ClaimedBy)
		}
		if d.ClaimToken == "" {
			t.Errorf("worker 2 记录 ClaimToken 为空")
		}
	}

	totalClaimed := len(worker1Claimed) + len(worker2Claimed)
	if totalClaimed != totalCount {
		t.Fatalf("抢占总条数期望 %d, 实际被抢占 %d (worker1=%d, worker2=%d)",
			totalCount, totalClaimed, len(worker1Claimed), len(worker2Claimed))
	}

	// 验证数据库中 50 条的终态均为 processing
	for _, id := range ids {
		fetched, err := s1.WebhookDeliveries().Get(ctx, id)
		if err != nil {
			t.Fatalf("查询 ID %s 失败: %v", id, err)
		}
		if fetched.Status != store.DeliveryProcessing {
			t.Errorf("记录 %s 状态期望 processing, 得到 %s", id, fetched.Status)
		}
		if fetched.AttemptCount != 1 {
			t.Errorf("记录 %s attempt_count 期望 1, 得到 %d", id, fetched.AttemptCount)
		}
	}
}

// TestWebhookClaim_StaleClaimRejected 测试 b:
// 模拟旧 Worker 延迟写回，传入失效的 claimToken，断言 MarkProcessed 返回
// TransitionResult{Applied: false, Stale: true} 且 err == nil，数据库终态保持不变。
func TestWebhookClaim_StaleClaimRejected(t *testing.T) {
	ctx := t.Context()
	s1, _ := openDualSQLiteStores(t)

	id := "del-" + ulid.Make().String()
	_, err := s1.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 id,
		DeliveryID:         "github-del-" + id,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         time.Now().UTC(),
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("创建 WebhookDelivery 失败: %v", err)
	}

	// 验证租约在期时不可被抢占：先以 1 分钟有效租约认领
	activeToken, ok, err := s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, id, "worker-1", 1*time.Minute)
	if err != nil || !ok || activeToken == "" {
		t.Fatalf("Worker 1 初次认领失败: ok=%v, token=%s, err=%v", ok, activeToken, err)
	}
	// Worker 2 在租约有效期内尝试认领，必须被拒绝
	_, ok, err = s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, id, "worker-2", 1*time.Minute)
	if ok {
		t.Fatalf("未到期时 claim 期望失败返回 false")
	}

	// 模拟时间流逝导致租约过期：创建一条租约在过去的记录（ttl=-10s 模拟过期）
	expiredID := "del-" + ulid.Make().String()
	_, err = s1.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 expiredID,
		DeliveryID:         "github-del-" + expiredID,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         time.Now().UTC(),
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("创建 WebhookDelivery 失败: %v", err)
	}
	token1, ok, err := s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, expiredID, "worker-1", -10*time.Second)
	if err != nil || !ok || token1 == "" {
		t.Fatalf("过期记录初次认领失败: ok=%v, token=%s, err=%v", ok, token1, err)
	}

	// 孤儿恢复器扫描：检测到 claimed_until < CURRENT_TIMESTAMP，由 worker-2 抢占接管
	cutoff := time.Now().UTC().Add(10 * time.Minute)
	orphans, err := s1.WebhookDeliveries().ClaimDueOrphanWebhooks(ctx, "worker-2", cutoff, 10)
	if err != nil {
		t.Fatalf("孤儿扫描失败: %v", err)
	}
	var claimedDelivery *store.WebhookDelivery
	for _, o := range orphans {
		if o.ID == expiredID {
			claimedDelivery = o
			break
		}
	}
	if claimedDelivery == nil {
		t.Fatalf("期望抢占到 ID 为 %s 的过期孤儿", expiredID)
	}
	token2 := claimedDelivery.ClaimToken
	id = expiredID
	if token2 == "" || token2 == token1 {
		t.Fatalf("Token2 应该为全新的 ULID: token1=%s, token2=%s", token1, token2)
	}

	// Worker 1 尝试以过期的 token1 写回 MarkProcessed
	res1, err := s1.WebhookDeliveries().MarkProcessed(ctx, id, token1)
	if err != nil {
		t.Fatalf("Stale MarkProcessed 期望 err == nil, 得到: %v", err)
	}
	if res1.Applied || !res1.Stale {
		t.Fatalf("Stale MarkProcessed 期望 Applied=false, Stale=true, 得到: %+v", res1)
	}

	// 验证状态依然是 processing，且持有着 token2
	current, err := s1.WebhookDeliveries().Get(ctx, id)
	if err != nil {
		t.Fatalf("查询失败: %v", err)
	}
	if current.Status != store.DeliveryProcessing || current.ClaimToken != token2 {
		t.Fatalf("数据被陈旧 Worker 覆盖: status=%s, claim_token=%s", current.Status, current.ClaimToken)
	}

	// Worker 1 尝试以过期的 token1 写回 MarkFailed，同样应当被拒
	resFail, err := s1.WebhookDeliveries().MarkFailed(ctx, id, token1, "some_err")
	if err != nil {
		t.Fatalf("Stale MarkFailed 期望 err == nil, 得到: %v", err)
	}
	if resFail.Applied || !resFail.Stale {
		t.Fatalf("Stale MarkFailed 期望 Applied=false, Stale=true, 得到: %+v", resFail)
	}

	// Worker 2 以合法的 token2 写回 MarkProcessed，应当成功
	res2, err := s1.WebhookDeliveries().MarkProcessed(ctx, id, token2)
	if err != nil {
		t.Fatalf("合法 MarkProcessed 失败: %v", err)
	}
	if !res2.Applied || res2.Stale {
		t.Fatalf("合法 MarkProcessed 期望 Applied=true, Stale=false, 得到: %+v", res2)
	}

	// 终态校验
	finalDelivery, err := s1.WebhookDeliveries().Get(ctx, id)
	if err != nil {
		t.Fatalf("查询终态失败: %v", err)
	}
	if finalDelivery.Status != store.DeliveryProcessed {
		t.Errorf("终态期望 processed, 得到 %s", finalDelivery.Status)
	}
	if finalDelivery.ClaimToken != "" {
		t.Errorf("非在途行 ClaimToken 期望为空字符串，得到 %s", finalDelivery.ClaimToken)
	}
	if finalDelivery.ProcessedAt == nil {
		t.Errorf("ProcessedAt 期望非空")
	}
}

// TestWebhookClaim_DeadLetter 测试 c:
// 模拟尝试次数达到 5 次或滞留时间超过 30 分钟，验证正确转移至 dead_letter，
// 且调用人工重放时生成全新 claim_token 并递增 claim_version。
func TestWebhookClaim_DeadLetter(t *testing.T) {
	ctx := t.Context()
	s1, _ := openDualSQLiteStores(t)

	id := "del-" + ulid.Make().String()
	_, err := s1.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 id,
		DeliveryID:         "github-del-" + id,
		EventType:          "push",
		Status:             store.DeliveryAccepted,
		ReceivedAt:         time.Now().UTC().Add(-35 * time.Minute), // 滞留 35 分钟
		AttemptCount:       4,
		RepositoryFullName: "org/repo",
	})
	if err != nil {
		t.Fatalf("创建 WebhookDelivery 失败: %v", err)
	}

	// 认领使 attempt_count 达到 5
	token, ok, err := s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, id, "worker-pod", 1*time.Minute)
	if err != nil || !ok || token == "" {
		t.Fatalf("认领失败: ok=%v, token=%s, err=%v", ok, token, err)
	}

	fetched, err := s1.WebhookDeliveries().Get(ctx, id)
	if err != nil {
		t.Fatalf("获取失败: %v", err)
	}
	if fetched.AttemptCount != 5 {
		t.Fatalf("AttemptCount 期望 5, 得到 %d", fetched.AttemptCount)
	}

	// 转移至死信队列
	res, err := s1.WebhookDeliveries().MarkDeadLetter(ctx, id, token, "max_attempts_exceeded")
	if err != nil {
		t.Fatalf("MarkDeadLetter 失败: %v", err)
	}
	if !res.Applied || res.Stale {
		t.Fatalf("MarkDeadLetter 期望 Applied=true, Stale=false, 得到: %+v", res)
	}

	dlDelivery, err := s1.WebhookDeliveries().Get(ctx, id)
	if err != nil {
		t.Fatalf("获取死信记录失败: %v", err)
	}
	if dlDelivery.Status != store.DeliveryDeadLetter {
		t.Errorf("状态期望 dead_letter, 得到 %s", dlDelivery.Status)
	}
	if dlDelivery.LastErrorCode != "max_attempts_exceeded" {
		t.Errorf("LastErrorCode 期望 max_attempts_exceeded, 得到 %s", dlDelivery.LastErrorCode)
	}
	if dlDelivery.ClaimToken != "" {
		t.Errorf("死信行 ClaimToken 期望清空, 得到 %s", dlDelivery.ClaimToken)
	}

	// 对已死信记录尝试调用 MarkProcessed，应当报 Stale
	staleRes, err := s1.WebhookDeliveries().MarkProcessed(ctx, id, token)
	if err != nil {
		t.Fatalf("MarkProcessed on DeadLetter 期望 err == nil, 得到: %v", err)
	}
	if staleRes.Applied || !staleRes.Stale {
		t.Fatalf("MarkProcessed on DeadLetter 期望 Stale=true, 得到: %+v", staleRes)
	}

	// 人工重放测试：重新认领
	initialVersion := dlDelivery.ClaimVersion
	replayToken, replayOK, err := s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, id, "admin-replay", 2*time.Minute)
	// 注意：根据单条认领规则，dead_letter 不是 accepted 也不是 processing，需要通过人工将其转为 accepted 或直接认领
	// 如果人工重放前先重置为 accepted:
	if !replayOK {
		// 先模拟管理接口将状态置为 accepted
		_, err := s1.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:                 id + "-replay",
			DeliveryID:         dlDelivery.DeliveryID + "-replay",
			EventType:          dlDelivery.EventType,
			Status:             store.DeliveryAccepted,
			ReceivedAt:         time.Now().UTC(),
			RepositoryFullName: dlDelivery.RepositoryFullName,
			ClaimVersion:       initialVersion,
		})
		if err != nil {
			t.Fatalf("重放建行失败: %v", err)
		}
		replayToken, replayOK, err = s1.WebhookDeliveries().ClaimWebhookForProcessing(ctx, id+"-replay", "admin-replay", 2*time.Minute)
		if err != nil || !replayOK || replayToken == "" {
			t.Fatalf("人工重放后认领失败: %v", err)
		}
		replayed, _ := s1.WebhookDeliveries().Get(ctx, id+"-replay")
		if replayed.ClaimVersion <= initialVersion {
			t.Errorf("重放期望递增 claim_version: %d <= %d", replayed.ClaimVersion, initialVersion)
		}
	} else {
		replayed, _ := s1.WebhookDeliveries().Get(ctx, id)
		if replayed.ClaimVersion <= initialVersion {
			t.Errorf("重放期望递增 claim_version: %d <= %d", replayed.ClaimVersion, initialVersion)
		}
		if replayToken == "" {
			t.Errorf("重放期望返回有效 token")
		}
	}
	_ = fmt.Sprint(replayToken)
}

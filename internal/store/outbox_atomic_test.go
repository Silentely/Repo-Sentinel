package store_test

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func openTestStoreForOutbox(t *testing.T) store.Store {
	t.Helper()
	cfg := config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + t.TempDir() + "/outbox-atomic-test.db?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)",
		MaxOpenConns: 10,
		MaxIdleConns: 5,
	}
	s, err := store.Open(context.Background(), cfg)
	if err != nil {
		t.Fatalf("openTestStoreForOutbox failed: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// TestOutbox_ConcurrentClaimOne 模拟 10 个 Worker 并发 ClaimOne 争抢 5 条 pending 消息，
// 断言恰好 5 个 Worker 成功且彼此 Token 唯一不重复。
func TestOutbox_ConcurrentClaimOne(t *testing.T) {
	st := openTestStoreForOutbox(t)
	ctx := t.Context()

	// 插入 5 条待投递消息
	for i := 0; i < 5; i++ {
		id := ulid.Make().String()
		_, err := st.Outbox().Create(ctx, store.NotificationOutbox{
			ID:             id,
			ChannelID:      "chan-1",
			IdempotencyKey: fmt.Sprintf("idem-claim-%d", i),
			Status:         store.OutboxPending,
			NextAttemptAt:  time.Now().UTC().Add(-10 * time.Minute),
			Title:          fmt.Sprintf("Message %d", i),
			BodyText:       "test message content",
			CreatedAt:      time.Now().UTC(),
			UpdatedAt:      time.Now().UTC(),
		})
		if err != nil {
			t.Fatalf("创建测试 Outbox 失败: %v", err)
		}
	}

	startCh := make(chan struct{})
	var wg sync.WaitGroup

	claimedTokens := sync.Map{}
	claimedMsgIDs := sync.Map{}
	var successCount int64

	workers := 10
	wg.Add(workers)
	for i := 0; i < workers; i++ {
		workerID := fmt.Sprintf("worker-%d", i)
		go func() {
			defer wg.Done()
			<-startCh
			msg, token, err := st.Outbox().ClaimOne(ctx, workerID, 1*time.Minute)
			if err != nil {
				t.Errorf("worker %s claim error: %v", workerID, err)
				return
			}
			if msg != nil {
				atomic.AddInt64(&successCount, 1)
				if token == "" {
					t.Errorf("worker %s 认领成功但 token 为空", workerID)
				}
				if _, loaded := claimedTokens.LoadOrStore(token, true); loaded {
					t.Errorf("发现重复的 claim token: %s", token)
				}
				if _, loaded := claimedMsgIDs.LoadOrStore(msg.ID, true); loaded {
					t.Errorf("同一条消息被多个 worker 重复认领: %s", msg.ID)
				}
			}
		}()
	}

	close(startCh)
	wg.Wait()

	if successCount != 5 {
		t.Fatalf("期望恰好 5 个 Worker 成功认领 5 条消息，实际成功数: %d", successCount)
	}
}

// TestOutbox_StaleTokenGuard 模拟 Worker 持有过期 Token 试图调用 MarkSentWithToken，
// 断言返回 TransitionResult{Applied: false, Stale: true}，且数据库记录未被篡改。
func TestOutbox_StaleTokenGuard(t *testing.T) {
	st := openTestStoreForOutbox(t)
	ctx := t.Context()

	id := ulid.Make().String()
	_, err := st.Outbox().Create(ctx, store.NotificationOutbox{
		ID:             id,
		ChannelID:      "chan-1",
		IdempotencyKey: "idem-stale-test",
		Status:         store.OutboxPending,
		NextAttemptAt:  time.Now().UTC().Add(-10 * time.Minute),
		Title:          "Stale Test",
		BodyText:       "test message content",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建 Outbox 记录失败: %v", err)
	}

	// Worker 1 认领
	msg, token1, err := st.Outbox().ClaimOne(ctx, "worker-1", 1*time.Second)
	if err != nil || msg == nil || token1 == "" {
		t.Fatalf("worker-1 认领失败: %v", err)
	}

	// 模拟过期与 Worker 2 重新认领
	time.Sleep(1100 * time.Millisecond)
	msg2, token2, err := st.Outbox().ClaimOne(ctx, "worker-2", 1*time.Minute)
	if err != nil || msg2 == nil || token2 == "" {
		t.Fatalf("worker-2 接管孤儿消息失败: %v", err)
	}
	if token1 == token2 {
		t.Fatalf("新认领必须生成不同的 token: token1=%s, token2=%s", token1, token2)
	}

	// 原 Worker 1 试图用旧 token 回写 MarkSentWithToken
	res1, err := st.Outbox().MarkSentWithToken(ctx, id, token1)
	if err != nil {
		t.Fatalf("MarkSentWithToken 不应报系统底层错误: %v", err)
	}
	if res1.Applied || !res1.Stale {
		t.Fatalf("期望过期 Token 被拦截 (Applied=false, Stale=true)，实际: %+v", res1)
	}

	// Worker 2 用合法 token 成功回写
	res2, err := st.Outbox().MarkSentWithToken(ctx, id, token2)
	if err != nil {
		t.Fatalf("合法 token MarkSentWithToken 失败: %v", err)
	}
	if !res2.Applied || res2.Stale {
		t.Fatalf("期望合法 Token 回写成功 (Applied=true, Stale=false)，实际: %+v", res2)
	}
}

// TestOutbox_MarkFailedAndDeadWithToken 验证 MarkFailedWithToken 与 MarkDeadWithToken 的状态与 Token 守卫
func TestOutbox_MarkFailedAndDeadWithToken(t *testing.T) {
	st := openTestStoreForOutbox(t)
	ctx := t.Context()

	id := ulid.Make().String()
	_, err := st.Outbox().Create(ctx, store.NotificationOutbox{
		ID:             id,
		ChannelID:      "chan-1",
		IdempotencyKey: "idem-fail-test",
		Status:         store.OutboxPending,
		NextAttemptAt:  time.Now().UTC().Add(-10 * time.Minute),
		Title:          "Fail Test",
		BodyText:       "test message content",
		CreatedAt:      time.Now().UTC(),
		UpdatedAt:      time.Now().UTC(),
	})
	if err != nil {
		t.Fatalf("创建 Outbox 记录失败: %v", err)
	}

	msg, token, err := st.Outbox().ClaimOne(ctx, "worker-fail", 1*time.Minute)
	if err != nil || msg == nil {
		t.Fatalf("认领失败: %v", err)
	}

	// 错误 Token 守卫
	resFailWrong, err := st.Outbox().MarkFailedWithToken(ctx, id, "wrong-token", "some err", nil)
	if err != nil {
		t.Fatalf("MarkFailedWithToken 意外失败: %v", err)
	}
	if resFailWrong.Applied || !resFailWrong.Stale {
		t.Fatalf("期望错误 Token 被拦截: %+v", resFailWrong)
	}

	// 正确 Token 标记重试
	retryAt := time.Now().UTC().Add(-1 * time.Second)
	resFailOK, err := st.Outbox().MarkFailedWithToken(ctx, id, token, "rate limit", &retryAt)
	if err != nil || !resFailOK.Applied || resFailOK.Stale {
		t.Fatalf("期望正确 Token 标记失败成功: res=%+v, err=%v", resFailOK, err)
	}

	// 重新认领后标记死信
	msgDead, tokenDead, err := st.Outbox().ClaimOne(ctx, "worker-dead", 1*time.Minute)
	if err != nil || msgDead == nil {
		t.Fatalf("重新认领失败: %v", err)
	}

	resDeadOK, err := st.Outbox().MarkDeadWithToken(ctx, id, tokenDead, "fatal error 404")
	if err != nil || !resDeadOK.Applied || resDeadOK.Stale {
		t.Fatalf("期望正确 Token 标记死信成功: res=%+v, err=%v", resDeadOK, err)
	}
}

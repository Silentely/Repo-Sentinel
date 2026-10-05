package store_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestWebhookDelivery_DehydratePayloads(t *testing.T) {
	st := openTestStore(t)

	ctx := context.Background()
	now := time.Now().UTC()
	cutoff := now.Add(-24 * time.Hour)

	// 1. 创建 10 条已处理且超过 24 小时的 Webhook 记录（符合脱水条件）
	for i := 1; i <= 10; i++ {
		procTime := cutoff.Add(-time.Duration(i) * time.Hour)
		_, err := st.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:                 fmt.Sprintf("del-old-%d", i),
			DeliveryID:         fmt.Sprintf("gh-del-old-%d", i),
			EventType:          "push",
			Action:             "",
			RepositoryFullName: "Silentely/Repo-Sentinel",
			Status:             store.DeliveryProcessed,
			Payload:            []byte(fmt.Sprintf(`{"ref":"refs/heads/main","commit":"%d"}`, i)),
			ReceivedAt:         procTime.Add(-5 * time.Second),
			ProcessedAt:        &procTime,
		})
		if err != nil {
			t.Fatalf("create old delivery failed: %v", err)
		}
	}

	// 2. 创建 2 条仍在处理中的记录（未完成，不应被脱水）
	for i := 1; i <= 2; i++ {
		procTime := cutoff.Add(-time.Duration(i) * time.Hour)
		_, err := st.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:         fmt.Sprintf("del-processing-%d", i),
			DeliveryID: fmt.Sprintf("gh-del-processing-%d", i),
			EventType:  "push",
			Status:     store.DeliveryProcessing,
			Payload:    []byte(`{"ref":"refs/heads/feat"}`),
			ReceivedAt: procTime,
		})
		if err != nil {
			t.Fatalf("create processing delivery failed: %v", err)
		}
	}

	// 3. 创建 2 条已处理但不足 24 小时的记录（新鲜记录，不应被脱水）
	for i := 1; i <= 2; i++ {
		procTime := now.Add(-time.Duration(i) * time.Hour)
		_, err := st.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:          fmt.Sprintf("del-fresh-%d", i),
			DeliveryID:  fmt.Sprintf("gh-del-fresh-%d", i),
			EventType:   "push",
			Status:      store.DeliveryProcessed,
			Payload:     []byte(`{"ref":"refs/heads/fresh"}`),
			ReceivedAt:  procTime.Add(-5 * time.Second),
			ProcessedAt: &procTime,
		})
		if err != nil {
			t.Fatalf("create fresh delivery failed: %v", err)
		}
	}

	// 4. 按 batchSize = 6 进行分批脱水
	totalDehydrated := 0
	for {
		n, err := st.WebhookDeliveries().DehydrateWebhookPayloads(ctx, cutoff, 6)
		if err != nil {
			t.Fatalf("DehydrateWebhookPayloads error: %v", err)
		}
		totalDehydrated += n
		if n < 6 {
			break
		}
	}

	if totalDehydrated != 10 {
		t.Fatalf("expected 10 dehydrated records, got %d", totalDehydrated)
	}

	// 5. 校验脱水后的老记录：payload 被清空，但元数据完整保留
	for i := 1; i <= 10; i++ {
		d, err := st.WebhookDeliveries().Get(ctx, fmt.Sprintf("del-old-%d", i))
		if err != nil {
			t.Fatalf("get old delivery %d failed: %v", i, err)
		}
		if len(d.Payload) != 0 {
			t.Errorf("expected empty payload for %s, got len %d", d.ID, len(d.Payload))
		}
		if d.Status != store.DeliveryProcessed || d.RepositoryFullName != "Silentely/Repo-Sentinel" || d.EventType != "push" {
			t.Errorf("metadata altered for %s: %+v", d.ID, d)
		}
	}

	// 6. 校验未完成和新鲜记录：payload 完好保留
	p1, err := st.WebhookDeliveries().Get(ctx, "del-processing-1")
	if err != nil || len(p1.Payload) == 0 {
		t.Errorf("processing record payload should be kept, got: len %d, err: %v", len(p1.Payload), err)
	}

	f1, err := st.WebhookDeliveries().Get(ctx, "del-fresh-1")
	if err != nil || len(f1.Payload) == 0 {
		t.Errorf("fresh record payload should be kept, got: len %d, err: %v", len(f1.Payload), err)
	}
}

func TestWebhookDelivery_Dehydrate_ConcurrentReplayConflict(t *testing.T) {
	st := openTestStore(t)
	ctx := context.Background()
	now := time.Now().UTC()
	cutoff := now.Add(-10 * time.Minute)

	// 创建 50 条测试记录
	for i := 1; i <= 50; i++ {
		procTime := cutoff.Add(-time.Duration(i) * time.Second)
		_, err := st.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
			ID:          fmt.Sprintf("del-conc-%d", i),
			DeliveryID:  fmt.Sprintf("gh-del-conc-%d", i),
			EventType:   "push",
			Status:      store.DeliveryProcessed,
			Payload:     []byte(fmt.Sprintf(`{"i":%d}`, i)),
			ReceivedAt:  procTime,
			ProcessedAt: &procTime,
		})
		if err != nil {
			t.Fatalf("create delivery failed: %v", err)
		}
	}

	done := make(chan struct{})

	// Goroutine 1: 执行分批脱水
	go func() {
		defer close(done)
		for {
			n, err := st.WebhookDeliveries().DehydrateWebhookPayloads(ctx, cutoff, 10)
			if err != nil {
				t.Errorf("dehydrate error: %v", err)
				return
			}
			if n == 0 {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
	}()

	// Goroutine 2: 并发尝试读取记录，验证脱水或未脱水状态的安全获取
	for i := 1; i <= 50; i++ {
		id := fmt.Sprintf("del-conc-%d", i)
		d, err := st.WebhookDeliveries().Get(ctx, id)
		if err != nil {
			t.Errorf("concurrent get delivery failed: %v", err)
			break
		}
		// 如果 payload 已经被脱水清空，校验其元数据依然正确
		if len(d.Payload) == 0 {
			if d.Status != store.DeliveryProcessed {
				t.Errorf("unexpected status on dehydrated item: %s", d.Status)
			}
		}
		time.Sleep(2 * time.Millisecond)
	}

	<-done
}

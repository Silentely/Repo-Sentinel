package jev_test

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
)

func TestLoadAndFaultInjection_Bounded(t *testing.T) {
	var totalRequests atomic.Int64
	var activeConcurrency atomic.Int64
	var peakConcurrency atomic.Int64
	var injectMode atomic.Int32 // 0: normal, 1: slow, 2: rate_limit_429, 3: overload_529, 4: malformed, 5: fail_500

	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		curr := activeConcurrency.Add(1)
		defer activeConcurrency.Add(-1)
		for {
			peak := peakConcurrency.Load()
			if curr <= peak || peakConcurrency.CompareAndSwap(peak, curr) {
				break
			}
		}

		totalRequests.Add(1)

		mode := injectMode.Load()
		switch mode {
		case 1: // slow body
			time.Sleep(50 * time.Millisecond)
		case 2: // 429
			w.Header().Set("Retry-After", "1")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"error":{"message":"rate limited"}}`))
			return
		case 3: // 529
			w.WriteHeader(529)
			_, _ = w.Write([]byte(`{"error":{"message":"site overloaded"}}`))
			return
		case 4: // malformed
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte(`{"malformed_json":`))
			return
		case 5: // 500
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = w.Write([]byte(`{"error":{"message":"internal error"}}`))
			return
		}

		w.Header().Set("Content-Type", "application/json")
		resp := map[string]any{
			"model": "jev-1.14.0",
			"answers": map[string]any{
				"verdict": map[string]any{"type": "noul", "noul": 0.05},
			},
			"usage": map[string]any{
				"input_tokens":  20,
				"output_tokens": 5,
			},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer fakeServer.Close()

	// 1. 初始化客户端（各面配置限流 max_concurrency=10）
	clientOpts := jev.ClientOptions{
		BaseURL:        fakeServer.URL,
		APIKey:         config.NewSecret("test-key"),
		Model:          "jev-1.14.0",
		Plane:          "notification",
		Timeout:        300 * time.Millisecond,
		MaxConcurrency: 10,
		HTTPClient:     fakeServer.Client(),
	}
	client, err := jev.NewClient(clientOpts)
	if err != nil {
		t.Fatalf("创建客户端失败: %v", err)
	}

	// 2. 验证并发限流：发射 50 个并发请求，上游峰值并发不得超过 MaxConcurrency (10)
	concurrencyTestCount := 50
	var wg sync.WaitGroup
	wg.Add(concurrencyTestCount)

	questions := map[string]jev.Question{
		"verdict": jev.NoulQuestion{Type: "noul", Instructions: "Is this spam?"},
	}
	state := json.RawMessage(`{"text":"hello world"}`)

	for i := 0; i < concurrencyTestCount; i++ {
		go func() {
			defer wg.Done()
			_, _ = client.Judge(context.Background(), state, questions)
		}()
	}
	wg.Wait()

	if peak := peakConcurrency.Load(); peak > 10 {
		t.Fatalf("上游峰值并发超过 max_concurrency(10): got %d", peak)
	}

	// 3. 故障注入：连续 5 次 500 触发熔断（使用唯一未缓存 state 保证打到上游）
	injectMode.Store(5)
	for i := 0; i < 5; i++ {
		failState := json.RawMessage(fmt.Sprintf(`{"fail_step":%d}`, i))
		_, _ = client.Judge(context.Background(), failState, questions)
	}

	// 熔断后，本地直接短路，不打向上游
	reqsBeforeOpen := totalRequests.Load()
	for i := 0; i < 5; i++ {
		unseenState := json.RawMessage(fmt.Sprintf(`{"unseen_during_open":%d}`, i))
		_, err := client.Judge(context.Background(), unseenState, questions)
		if err == nil {
			t.Fatal("熔断开启时应当报错")
		}
	}
	if reqsAfterOpen := totalRequests.Load(); reqsAfterOpen != reqsBeforeOpen {
		t.Fatalf("熔断生效期间不应发起远程请求: before=%d, after=%d", reqsBeforeOpen, reqsAfterOpen)
	}

	// 4. 双实例测试：两个独立 Client 实例使用相同 state，验证本地缓存/限制在实例间隔离
	client2, err := jev.NewClient(clientOpts)
	if err != nil {
		t.Fatalf("创建 client2 失败: %v", err)
	}
	injectMode.Store(0) // 恢复正常

	// 跨实例调用相同 state，分别产生独立远程调用（单实例单飞/缓存不跨进程越界）
	freshState := json.RawMessage(`{"fresh":"instance-isolation-check"}`)
	reqsBefore2 := totalRequests.Load()
	_, _ = client2.Judge(context.Background(), freshState, questions)
	if totalRequests.Load() <= reqsBefore2 {
		t.Fatal("独立 client2 实例应当能够发起自己的远程调用")
	}

	// 5. 三面隔离测试：Review 面处于故障状态，Notification 和 ChatOps 面依然正常可用
	reviewClientOpts := clientOpts
	reviewClientOpts.Plane = "review"
	reviewClient, err := jev.NewClient(reviewClientOpts)
	if err != nil {
		t.Fatalf("创建 reviewClient 失败: %v", err)
	}

	chatopsClientOpts := clientOpts
	chatopsClientOpts.Plane = "chatops"
	chatopsClient, err := jev.NewClient(chatopsClientOpts)
	if err != nil {
		t.Fatalf("创建 chatopsClient 失败: %v", err)
	}

	// 让 Review 客户端连续 5 次失败触发熔断
	injectMode.Store(5)
	for i := 0; i < 5; i++ {
		s := json.RawMessage(fmt.Sprintf(`{"review_fail":%d}`, i))
		_, _ = reviewClient.Judge(context.Background(), s, questions)
	}

	// 恢复服务器为正常状态，验证 Notification 与 ChatOps 未受影响
	injectMode.Store(0)
	notifState := json.RawMessage(`{"notif":"after_review_failure"}`)
	chatopsState := json.RawMessage(`{"chatops":"after_review_failure"}`)

	if _, err := client2.Judge(context.Background(), notifState, questions); err != nil {
		t.Fatalf("Review 面故障不应影响 Notification 面: %v", err)
	}
	if _, err := chatopsClient.Judge(context.Background(), chatopsState, questions); err != nil {
		t.Fatalf("Review 面故障不应影响 ChatOps 面: %v", err)
	}

	// 长负载压力测试由 REPOSENTINEL_JEV_LOAD_TEST=1 显式触发
	if os.Getenv("REPOSENTINEL_JEV_LOAD_TEST") == "1" {
		t.Log("执行 30s 持续高并发负载与故障演练...")
		deadline := time.Now().Add(30 * time.Second)
		var soakWg sync.WaitGroup
		for i := 0; i < 100; i++ {
			soakWg.Add(1)
			go func() {
				defer soakWg.Done()
				for time.Now().Before(deadline) {
					_, _ = client2.Judge(context.Background(), state, questions)
					time.Sleep(10 * time.Millisecond)
				}
			}()
		}
		soakWg.Wait()
		t.Log("30s 持续高并发测试完成，未发生 panic 或死锁")
	}
}

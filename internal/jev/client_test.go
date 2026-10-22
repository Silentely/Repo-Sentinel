package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func TestClientBasicTLSAndProtocol(t *testing.T) {
	respBytes, err := os.ReadFile("testdata/systemone.response.json")
	if err != nil {
		t.Fatal(err)
	}

	var reqCount int
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		reqCount++
		if r.Method != http.MethodPost {
			t.Fatalf("unexpected method: %s", r.Method)
		}
		if r.URL.Path != "/v1/systemone" {
			t.Fatalf("unexpected path: %s", r.URL.Path)
		}
		if r.Header.Get("Authorization") != "Bearer fixture-key" {
			t.Fatalf("unexpected auth: %s", r.Header.Get("Authorization"))
		}
		if r.Header.Get("Content-Type") != "application/json" {
			t.Fatalf("unexpected content-type: %s", r.Header.Get("Content-Type"))
		}

		body, _ := io.ReadAll(r.Body)
		var req map[string]json.RawMessage
		if err := json.Unmarshal(body, &req); err != nil {
			t.Fatalf("request json invalid: %v", err)
		}
		if string(req["model"]) != `"jev-1.14.0"` {
			t.Fatalf("unexpected model in body: %s", string(req["model"]))
		}

		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	opts := ClientOptions{
		BaseURL:              ts.URL,
		APIKey:               config.NewSecret("fixture-key"),
		Model:                "jev-1.14.0",
		Plane:                "notification",
		PolicyVersion:        "jev-policy-v1",
		CredentialGeneration: 1,
		RuntimeGeneration:    1,
		Timeout:              500 * time.Millisecond,
		MaxConcurrency:       8,
		Retries:              1,
		HTTPClient:           ts.Client(),
	}

	client, err := NewClient(opts)
	if err != nil {
		t.Fatalf("NewClient failed: %v", err)
	}

	q := map[string]Question{
		"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check action"},
	}
	state := json.RawMessage(`{"kind":"workflow_run","conclusion":"failure"}`)

	res, err := client.Judge(t.Context(), state, q)
	if err != nil {
		t.Fatalf("Judge failed: %v", err)
	}
	if res.Model != "jev-1.14.0" {
		t.Fatalf("unexpected res model: %s", res.Model)
	}
	if reqCount != 1 {
		t.Fatalf("expected 1 remote call, got %d", reqCount)
	}
}

func TestSingleflightAndCancellation(t *testing.T) {
	respBytes, _ := os.ReadFile("testdata/systemone.response.json")
	var remoteCalls atomic.Int32

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		remoteCalls.Add(1)
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "notification",
		Timeout:        800 * time.Millisecond,
		MaxConcurrency: 8,
		HTTPClient:     ts.Client(),
	}
	client, _ := NewClient(opts)

	q := map[string]Question{
		"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"},
	}
	state := json.RawMessage(`{"job":"build"}`)

	// 10 个相同并发请求
	var wg sync.WaitGroup
	var successCount atomic.Int32
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
			if idx == 0 {
				// leader 故意提前取消，验证不牵连其他等待者
				go func() {
					time.Sleep(20 * time.Millisecond)
					cancel()
				}()
			} else {
				defer cancel()
			}
			_, err := client.Judge(ctx, state, q)
			if err == nil {
				successCount.Add(1)
			}
		}(i)
	}
	wg.Wait()

	if calls := remoteCalls.Load(); calls != 1 {
		t.Fatalf("Singleflight 必须只触发 1 次远程请求，实际: %d", calls)
	}
	if successCount.Load() < 8 {
		t.Fatalf("除取消的调用者外，其余等待者应成功，成功数: %d", successCount.Load())
	}
}

func TestSemaphoreConcurrencyLimit(t *testing.T) {
	respBytes, _ := os.ReadFile("testdata/systemone.response.json")

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(100 * time.Millisecond)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	// 容量 1
	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "chatops", // chatops 跳过 singleflight，直接测信号量
		Timeout:        500 * time.Millisecond,
		MaxConcurrency: 1,
		HTTPClient:     ts.Client(),
	}
	client, _ := NewClient(opts)

	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}
	state := json.RawMessage(`{"a":1}`)

	var wg sync.WaitGroup
	wg.Add(2)

	go func() {
		defer wg.Done()
		_, _ = client.Judge(context.Background(), state, q)
	}()

	var err2 error
	go func() {
		defer wg.Done()
		time.Sleep(10 * time.Millisecond)
		// 短超时导致在信号量排队阶段超时
		ctx2, cancel2 := context.WithTimeout(context.Background(), 30*time.Millisecond)
		defer cancel2()
		_, err2 = client.Judge(ctx2, state, q)
	}()

	wg.Wait()
	if !errors.Is(err2, context.DeadlineExceeded) {
		t.Fatalf("排队超时应返回 DeadlineExceeded: %v", err2)
	}

	// 再次调用，槽位必须已归还且能正常工作
	ctx3, cancel3 := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel3()
	if _, err := client.Judge(ctx3, state, q); err != nil {
		t.Fatalf("信号量槽位未归还或泄漏: %v", err)
	}
}

func TestClientRetryAnd4xxNoRetry(t *testing.T) {
	var attempts atomic.Int32
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		a := attempts.Add(1)
		if a == 1 {
			w.WriteHeader(http.StatusServiceUnavailable) // 503 瞬态故障
			return
		}
		respBytes, _ := os.ReadFile("testdata/systemone.response.json")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "chatops",
		Timeout:        600 * time.Millisecond,
		MaxConcurrency: 4,
		Retries:        1,
		HTTPClient:     ts.Client(),
		Jitter:         func() float64 { return 0 },
	}
	client, _ := NewClient(opts)
	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}

	// 503 重试后成功，共 2 次请求
	if _, err := client.Judge(context.Background(), json.RawMessage(`{}`), q); err != nil {
		t.Fatalf("重试后应成功: %v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("预期 2 次请求，实际: %d", attempts.Load())
	}

	// 401/403/422 客户端错误不重试
	attempts.Store(0)
	ts4xx := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts.Add(1)
		w.WriteHeader(http.StatusUnprocessableEntity) // 422
	}))
	defer ts4xx.Close()

	opts.BaseURL = ts4xx.URL
	opts.HTTPClient = ts4xx.Client()
	client4xx, _ := NewClient(opts)

	if _, err := client4xx.Judge(context.Background(), json.RawMessage(`{}`), q); err == nil {
		t.Fatal("422 应返回错误")
	}
	if attempts.Load() != 1 {
		t.Fatalf("4xx 严禁重试，实际请求了 %d 次", attempts.Load())
	}
}

func TestRetryAfterAndRateLimiting(t *testing.T) {
	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Retry-After", "2") // 2 秒冷却
		w.WriteHeader(http.StatusTooManyRequests)
	}))
	defer ts.Close()

	now := time.Unix(1000, 0)
	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "chatops",
		Timeout:        500 * time.Millisecond,
		MaxConcurrency: 4,
		Retries:        1,
		HTTPClient:     ts.Client(),
		Now:            func() time.Time { return now },
	}
	client, _ := NewClient(opts)
	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}

	// 遇到 429，因为 2s 远超预算 500ms，直接快速失败返回 ErrRateLimited
	_, err := client.Judge(context.Background(), json.RawMessage(`{}`), q)
	if !errors.Is(err, ErrRateLimited) {
		t.Fatalf("超出预算的限流应立即返回 ErrRateLimited: %v", err)
	}

	// 后续请求在冷却期未过时 fast-fallback
	_, err2 := client.Judge(context.Background(), json.RawMessage(`{}`), q)
	if !errors.Is(err2, ErrRateLimited) {
		t.Fatalf("冷却期内应直接返回 ErrRateLimited: %v", err2)
	}
}

func TestPlaneIsolation(t *testing.T) {
	respBytes, _ := os.ReadFile("testdata/systemone.response.json")

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	// review 面故意配置极小并发且全部占满
	optsReview := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "review",
		Timeout:        300 * time.Millisecond,
		MaxConcurrency: 1,
		HTTPClient:     ts.Client(),
	}
	clientReview, _ := NewClient(optsReview)

	optsNotification := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "notification",
		Timeout:        500 * time.Millisecond,
		MaxConcurrency: 8,
		HTTPClient:     ts.Client(),
	}
	clientNotification, _ := NewClient(optsNotification)

	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}

	// 触发 review 熔断
	rImpl := clientReview.(*clientImpl)
	for i := 0; i < 5; i++ {
		rImpl.breaker.record(false)
	}
	if _, err := clientReview.Judge(context.Background(), json.RawMessage(`{}`), q); !errors.Is(err, ErrCircuitOpen) {
		t.Fatalf("review 应已熔断: %v", err)
	}

	// notification 面完全不受影响，正常执行
	if _, err := clientNotification.Judge(context.Background(), json.RawMessage(`{}`), q); err != nil {
		t.Fatalf("notification 面不应受 review 熔断影响: %v", err)
	}
}

func TestClientContextCancellationImmediatelyInterrupts(t *testing.T) {
	requestStarted := make(chan struct{})
	testDone := make(chan struct{})

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		close(requestStarted)
		// 阻塞等待测试结束或上层主动取消
		select {
		case <-testDone:
		case <-r.Context().Done():
		case <-time.After(5 * time.Second):
		}
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"model":"m"}`))
	}))
	defer ts.Close()
	defer close(testDone)

	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "chatops",
		Timeout:        10 * time.Second,
		MaxConcurrency: 4,
		HTTPClient:     ts.Client(),
	}
	client, err := NewClient(opts)
	if err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}

	done := make(chan error, 1)
	start := time.Now()
	go func() {
		_, err := client.Judge(ctx, json.RawMessage(`{}`), q)
		done <- err
	}()

	// 等待服务端收到请求
	<-requestStarted
	// 上层主动取消
	cancel()

	// 验证 Judge 立刻中断返回 context.Canceled，而不是等待服务端的 5 秒
	select {
	case err := <-done:
		cost := time.Since(start)
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("expected context.Canceled, got %v", err)
		}
		if cost >= 1*time.Second {
			t.Fatalf("cancellation took too long: %v", cost)
		}
	case <-time.After(1 * time.Second):
		t.Fatal("Judge did not cancel promptly")
	}
}

func TestClientSensitiveAndTruncatedBypassCache(t *testing.T) {
	respBytes, _ := os.ReadFile("testdata/systemone.response.json")
	var calls atomic.Int32

	ts := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(respBytes)
	}))
	defer ts.Close()

	opts := ClientOptions{
		BaseURL:        ts.URL,
		Model:          "jev-1.14.0",
		Plane:          "notification",
		Timeout:        1 * time.Second,
		MaxConcurrency: 4,
		HTTPClient:     ts.Client(),
	}
	client, _ := NewClient(opts)
	q := map[string]Question{"is_actionable": NoulQuestion{Type: "noul", Instructions: "Check"}}

	// 1. 敏感请求：两次相同请求均应透传到底层服务器，不入缓存
	sensitiveState := json.RawMessage(`{"sensitive":true,"kind":"pr"}`)
	_, err := client.Judge(context.Background(), sensitiveState, q)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Judge(context.Background(), sensitiveState, q)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 2 {
		t.Fatalf("sensitive request must bypass cache; expected 2 calls, got %d", calls.Load())
	}

	// 2. 规则 6：截断必须保守终局，截断请求严禁自动放行，必须判定为失败并升级人工
	truncatedState := json.RawMessage(`{"truncated":true,"kind":"push"}`)
	_, err = client.Judge(context.Background(), truncatedState, q)
	if !errors.Is(err, ErrTruncatedState) {
		t.Fatalf("expected ErrTruncatedState, got %v", err)
	}
	// 截断请求在客户端直接安全拦截，不发起下游网络请求
	if calls.Load() != 2 {
		t.Fatalf("truncated request must be rejected directly before network; expected 2 calls, got %d", calls.Load())
	}

	// 3. 正常请求：第 1 次调后端，第 2 次走缓存
	normalState := json.RawMessage(`{"kind":"push","id":"123"}`)
	_, err = client.Judge(context.Background(), normalState, q)
	if err != nil {
		t.Fatal(err)
	}
	_, err = client.Judge(context.Background(), normalState, q)
	if err != nil {
		t.Fatal(err)
	}
	if calls.Load() != 3 {
		t.Fatalf("normal request should hit cache on second call; expected 3 calls, got %d", calls.Load())
	}
}

func TestClassifyError(t *testing.T) {
	cases := []struct {
		err      error
		expected string
	}{
		{nil, ""},
		{ErrCircuitOpen, "breaker_open"},
		{ErrRateLimited, "rate_limited"},
		{context.DeadlineExceeded, "timeout"},
		{context.Canceled, "timeout"},
		{errors.New("connection timeout"), "timeout"},
		{ErrMalformedDecision, "decode"},
		{ErrStateTooLarge, "decode"},
		{errors.New("json: cannot unmarshal string into Go struct"), "decode"},
		{errors.New("jev: client error 404: not found"), "4xx"},
		{errors.New("jev: client error 422: unprocessable"), "4xx"},
		{errors.New("jev: server returned 500: internal error"), "5xx"},
		{errors.New("jev: server returned 503"), "5xx"},
		{errors.New("something totally arbitrary"), "unknown"},
	}

	for _, c := range cases {
		got := ClassifyError(c.err)
		if got != c.expected {
			t.Errorf("ClassifyError(%v) = %q; want %q", c.err, got, c.expected)
		}
	}
}

func TestNormalizeEndpoint(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"https://api.example.com", "https://api.example.com/v1/systemone"},
		{"https://api.example.com/", "https://api.example.com/v1/systemone"},
		{"https://api.example.com/v1", "https://api.example.com/v1/systemone"},
		{"https://api.example.com/v1/", "https://api.example.com/v1/systemone"},
		{"https://api.example.com/v1/systemone", "https://api.example.com/v1/systemone"},
		{"https://api.example.com/v1/systemone/", "https://api.example.com/v1/systemone"},
		{"https://proxy.example.com/custom/path", "https://proxy.example.com/custom/path/v1/systemone"},
	}

	for _, c := range cases {
		got := NormalizeEndpoint(c.input)
		if got != c.expected {
			t.Errorf("NormalizeEndpoint(%q) = %q; want %q", c.input, got, c.expected)
		}
	}
}

func TestClientUpstreamEchoHeaderLeakingPrevented(t *testing.T) {
	// 模拟上游故意在 500 / 401 错误响应中回显 Authorization Header
	sensitiveToken := "Bearer super-secret-leaked-api-token"
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(fmt.Sprintf(`{"error": "forbidden", "echoed_auth": "%s"}`, sensitiveToken)))
	}))
	defer ts.Close()

	client, err := NewClient(ClientOptions{
		BaseURL:    ts.URL,
		Plane:      "notification",
		Timeout:    2 * time.Second,
		HTTPClient: ts.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	q := map[string]Question{
		"test": NoulQuestion{Type: "noul", Instructions: "test"},
	}
	_, err = client.Judge(context.Background(), json.RawMessage(`{"kind":"test"}`), q)
	if err == nil {
		t.Fatal("expected judge to fail on 500")
	}

	errMsg := err.Error()
	if strings.Contains(errMsg, sensitiveToken) || strings.Contains(errMsg, "super-secret-leaked-api-token") {
		t.Fatalf("CRITICAL SECURITY LEAK: error message contains upstream response body: %s", errMsg)
	}
	if !strings.Contains(errMsg, "server returned 500") {
		t.Fatalf("expected generic server returned 500, got: %s", errMsg)
	}
}

func TestClientCacheEvictionMetricRecord(t *testing.T) {
	// 验证容量上限淘汰时触发 RecordCacheEviction
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]any{
			"model": "jev-1.14.0",
			"answers": map[string]any{
				"test": map[string]any{
					"type": "noul",
					"noul": 0.05,
				},
			},
			"usage": map[string]any{"input_tokens": 10, "output_tokens": 2},
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer ts.Close()

	client, err := NewClient(ClientOptions{
		BaseURL:    ts.URL,
		Plane:      "probe",
		Timeout:    2 * time.Second,
		HTTPClient: ts.Client(),
	})
	if err != nil {
		t.Fatal(err)
	}

	impl := client.(*clientImpl)
	// 将容量设小以便测试淘汰
	impl.cache.capacity = 2

	q := map[string]Question{
		"test": NoulQuestion{Type: "noul", Instructions: "test"},
	}
	for i := 1; i <= 3; i++ {
		state := json.RawMessage(fmt.Sprintf(`{"id": %d}`, i))
		_, err := client.Judge(context.Background(), state, q)
		if err != nil {
			t.Fatal(err)
		}
	}

	snap := GlobalMetrics().Snapshot()
	if snap.CacheEvicts["probe"] < 1 {
		t.Fatalf("expected at least 1 cache eviction in probe plane, got %d", snap.CacheEvicts["probe"])
	}
}

package httpapi

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestSystemHealth_ComprehensiveDiagnostics(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	ctx := context.Background()
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-1", ChannelID: "chan-1", IdempotencyKey: "idem-1", Status: "pending",
	})
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-2", ChannelID: "chan-1", IdempotencyKey: "idem-2", Status: "sent",
	})
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-3", ChannelID: "chan-1", IdempotencyKey: "idem-3", Status: "dead",
	})

	rec := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45201", cookies, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", rec.Code, rec.Body.String())
	}

	var res systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !res.DatabaseOK {
		t.Fatalf("expected database_ok=true")
	}
	if res.Status != "ok" {
		t.Fatalf("expected status=\"ok\", got %q", res.Status)
	}
	if res.Storage.Driver != "sqlite" {
		t.Fatalf("expected storage.driver=\"sqlite\", got %q", res.Storage.Driver)
	}
	if res.Storage.FileSizeBytes < 0 {
		t.Fatalf("expected file_size_bytes >= 0, got %d", res.Storage.FileSizeBytes)
	}
	if res.Outbox.PendingCount != 1 || res.Outbox.DeliveredCount != 1 || res.Outbox.DeadCount != 1 {
		t.Fatalf("expected outbox counts pending=1, delivered=1, dead=1; got %+v", res.Outbox)
	}
	if res.AIBudget.IsThrottled {
		t.Fatalf("expected ai_budget.is_throttled=false initially")
	}
}

type pingFailStore struct {
	store.Store
}

func (p *pingFailStore) PingQuick(ctx context.Context) error {
	return errors.New("db connection failure")
}

func TestSystemHealth_DatabaseFailureReturns503(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{
		decorateStore: func(s store.Store) store.Store {
			return &pingFailStore{Store: s}
		},
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	rec := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45202", cookies, nil)
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected 503 Service Unavailable, got %d: %s", rec.Code, rec.Body.String())
	}

	var res systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if res.DatabaseOK {
		t.Fatalf("expected database_ok=false on ping failure")
	}
}

func TestSystemHealth_AIBudgetThrottledDegradesTo200(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	ctx := context.Background()
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-1", ChannelID: "chan-1", IdempotencyKey: "idem-1", Status: "pending",
	})
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-2", ChannelID: "chan-1", IdempotencyKey: "idem-2", Status: "sent",
	})
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID: "out-diag-3", ChannelID: "chan-1", IdempotencyKey: "idem-3", Status: "dead",
	})

	// Artificially trigger AI throttling for today
	todayKey := "ai_budget:" + time.Now().UTC().Format("2006-01-02")
	_, err := fixture.store.Settings().UpdateAIBudgetUsageAtomic(context.Background(), todayKey, 10000, 600, 500)
	if err != nil {
		t.Fatalf("failed to update ai budget: %v", err)
	}

	rec := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45203", cookies, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK (graceful degradation), got %d: %s", rec.Code, rec.Body.String())
	}

	var res systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if !res.DatabaseOK {
		t.Fatalf("expected database_ok=true")
	}
	if res.Status != "degraded" {
		t.Fatalf("expected status=\"degraded\" when AI is throttled, got %q", res.Status)
	}
	if !res.AIBudget.IsThrottled {
		t.Fatalf("expected ai_budget.is_throttled=true")
	}
}

func TestSystemHealth_GitHubTimeoutDegradesTo200(t *testing.T) {
	// Start an unresponsive or failing test server for GitHub rate limit
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "github api outage", http.StatusBadGateway)
	}))
	defer ts.Close()

	ghClient := &githubx.AppClient{
		AppID:   12345,
		BaseURL: ts.URL,
	}
	ghClient.Configure(12345, "", generateTestRSAKey(t))

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	rec := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45204", cookies, nil)
	if rec.Code != http.StatusOK {
		t.Fatalf("expected 200 OK (graceful degradation), got %d: %s", rec.Code, rec.Body.String())
	}

	var res systemHealthResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if !res.DatabaseOK {
		t.Fatalf("expected database_ok=true")
	}
	if res.Status != "degraded" {
		t.Fatalf("expected status=\"degraded\" when GitHub probe fails, got %q", res.Status)
	}
	if res.GitHub.Status != "degraded" {
		t.Fatalf("expected github.status=\"degraded\", got %q", res.GitHub.Status)
	}
}

func generateTestRSAKey(t *testing.T) string {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)}))
}

func TestSystemHealth_GitHubProbeCached(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resources": map[string]any{
				"core": map[string]any{
					"limit":     5000,
					"remaining": 4888,
				},
			},
		})
	}))
	defer ts.Close()

	ghClient := &githubx.AppClient{
		AppID:   12345,
		BaseURL: ts.URL,
	}
	ghClient.Configure(12345, "", generateTestRSAKey(t))

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 第 1 次请求：发起探针
	rec1 := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45205", cookies, nil)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec1.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected 1 call to GitHub API, got %d", callCount)
	}

	// 第 2 次请求：60秒内复用缓存，不额外发起网络调用
	rec2 := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45206", cookies, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d", rec2.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected callCount to remain 1 due to cache, got %d", callCount)
	}

	var res systemHealthResponse
	if err := json.Unmarshal(rec2.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}
	if res.GitHub.RateLimitRemaining != 4888 {
		t.Fatalf("expected cached rate_limit_remaining=4888, got %d", res.GitHub.RateLimitRemaining)
	}
}

func TestSystemHealth_GitHubProbeSingleFlight(t *testing.T) {
	var callCount atomic.Int32
	probeGate := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount.Add(1)
		<-probeGate // 阻塞直到并发请求全部发出
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resources": map[string]any{
				"core": map[string]any{
					"limit":     5000,
					"remaining": 4777,
				},
			},
		})
	}))
	defer ts.Close()

	ghClient := &githubx.AppClient{
		AppID:   12345,
		BaseURL: ts.URL,
	}
	ghClient.Configure(12345, "", generateTestRSAKey(t))

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 启动 10 个并发请求，验证 singleflight 合并只发起一次真实 probe
	const concurrency = 10
	var wg sync.WaitGroup
	wg.Add(concurrency)

	for i := 0; i < concurrency; i++ {
		go func(idx int) {
			defer wg.Done()
			rec := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", fmt.Sprintf("127.0.0.1:453%02d", idx), cookies, nil)
			if rec.Code != http.StatusOK {
				t.Errorf("request %d returned %d", idx, rec.Code)
			}
		}(i)
	}

	// 稍作等待确保并发协程均已进入 singleflight 排队
	time.Sleep(50 * time.Millisecond)
	close(probeGate)
	wg.Wait()

	if got := callCount.Load(); got != 1 {
		t.Fatalf("expected singleflight to make exactly 1 probe call, got %d", got)
	}
}

func TestSystemHealth_GitHubConfigUpdateClearsCache(t *testing.T) {
	callCount := 0
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		callCount++
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"resources": map[string]any{
				"core": map[string]any{
					"limit":     5000,
					"remaining": 4000 + callCount,
				},
			},
		})
	}))
	defer ts.Close()

	ghClient := &githubx.AppClient{
		AppID:   12345,
		BaseURL: ts.URL,
	}
	ghClient.Configure(12345, "", generateTestRSAKey(t))

	fixture := newHTTPTestFixture(t, httpTestOptions{
		githubClient: ghClient,
		keyRing:      testHTTPKeyRing(t),
	})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 第一次请求进入缓存
	rec1 := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45401", cookies, nil)
	if rec1.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", rec1.Code)
	}
	if callCount != 1 {
		t.Fatalf("expected callCount 1, got %d", callCount)
	}

	// 更新 GitHub 配置，触发缓存清理
	csrf := cookieByName(t, cookies, CSRFCookieName)
	if csrf == nil {
		t.Fatal("missing csrf cookie")
	}
	putData, _ := json.Marshal(map[string]any{
		"app_id":         int64(12345),
		"client_id":      "cli-2",
		"private_key_pem":    generateTestRSAKey(t),
		"webhook_secret": "new-secret",
	})
	recPut := fixture.request(t, http.MethodPut, "/api/v1/github/config", string(putData), "127.0.0.1:45402", cookies, map[string]string{
		CSRFHeaderName: csrf.Value,
	})
	if recPut.Code != http.StatusOK {
		t.Fatalf("expected 200 on config update, got %d: %s", recPut.Code, recPut.Body.String())
	}

	// 第二次请求应当穿透重新探测，因为缓存已在配置更新时清空
	rec2 := fixture.request(t, http.MethodGet, "/api/v1/system/health", "", "127.0.0.1:45403", cookies, nil)
	if rec2.Code != http.StatusOK {
		t.Fatalf("expected 200 on health after update, got %d", rec2.Code)
	}
	if callCount != 2 {
		t.Fatalf("expected callCount to be 2 after config update invalidated cache, got %d", callCount)
	}
}

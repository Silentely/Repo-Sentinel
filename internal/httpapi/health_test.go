package httpapi

import (
	"context"
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
	if res.Outbox.PendingCount < 0 || res.Outbox.DeadCount < 0 {
		t.Fatalf("unexpected outbox counts: %+v", res.Outbox)
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

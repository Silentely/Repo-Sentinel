package httpapi

import (
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// doRequest 向指定 handler 发送带 Session Cookie 与 CSRF 头的 JSON 请求。
// 与夹具的 request 不同，这里可指向注入自定义 SSEHub 的 handler。
func doRequest(
	t *testing.T,
	handler http.Handler,
	method, path, body string,
	cookies []*http.Cookie,
	headers map[string]string,
) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	req.RemoteAddr = "127.0.0.1:45999"
	req.Header.Set("Content-Type", "application/json")
	for _, c := range cookies {
		req.AddCookie(c)
	}
	for name, value := range headers {
		req.Header.Set(name, value)
	}
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	return rec
}

// TestWorkItemIgnoreBroadcastsSSE 验证忽略标记写入后确实向 SSE 订阅者广播
// work_items.changed：单条 PATCH 与批量端点都要广播，否则其他标签页看到陈旧状态。
func TestWorkItemIgnoreBroadcastsSSE(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-sse-broadcast",
		FullName:       "org/sse-broadcast",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	for i, id := range []string{"wi-sse-1", "wi-sse-2"} {
		if _, _, err := fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
			ID:              id,
			RepositoryID:    repo.ID,
			Number:          i + 1,
			Kind:            store.WorkItemKindIssue,
			State:           "open",
			Title:           "sse",
			Author:          "dev",
			SourceUpdatedAt: time.Now().UTC(),
			StateHash:       "hash-" + id,
		}, nil); err != nil {
			t.Fatalf("upsert item %s: %v", id, err)
		}
	}

	hub := NewSSEHub(slog.New(slog.NewJSONHandler(io.Discard, nil)))
	handler := New(Dependencies{
		Config:         config.Config{HTTP: config.HTTPConfig{PublicBaseURL: "https://reposentinel.example"}},
		Store:          fixture.store,
		AdminService:   fixture.adminService,
		SessionService: fixture.sessionService,
		Logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		SSEHub:         hub,
	})
	ch, clientID, err := hub.Subscribe()
	if err != nil {
		t.Fatalf("subscribe: %v", err)
	}
	defer hub.Unsubscribe(clientID)

	expectEvent := func(stage string) SSEEvent {
		t.Helper()
		select {
		case evt := <-ch:
			if evt.Topic != "work_items.changed" || evt.Resource != "work_item" {
				t.Fatalf("%s: 广播内容不符，topic=%q resource=%q", stage, evt.Topic, evt.Resource)
			}
			return evt
		case <-time.After(3 * time.Second):
			t.Fatalf("%s: 未收到 SSE 广播", stage)
			return SSEEvent{}
		}
	}

	// 单条 PATCH
	single := doRequest(t, handler, http.MethodPatch, "/api/v1/work-items/wi-sse-1/ignored",
		`{"ignored":true}`, cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if single.Code != http.StatusOK {
		t.Fatalf("单条忽略状态=%d body=%s", single.Code, single.Body.String())
	}
	if evt := expectEvent("单条 PATCH"); evt.ResourceID != "wi-sse-1" {
		t.Fatalf("单条 PATCH 广播的 resource_id=%q，期望 wi-sse-1", evt.ResourceID)
	}

	// 批量端点：每个 ID 各广播一条
	batch := doRequest(t, handler, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":["wi-sse-1","wi-sse-2"],"ignored":true}`, cookies, map[string]string{CSRFHeaderName: csrf.Value})
	if batch.Code != http.StatusOK {
		t.Fatalf("批量忽略状态=%d body=%s", batch.Code, batch.Body.String())
	}
	got := map[string]bool{}
	for i := 0; i < 2; i++ {
		got[expectEvent("批量端点").ResourceID] = true
	}
	if !got["wi-sse-1"] || !got["wi-sse-2"] {
		t.Fatalf("批量端点广播的 resource_id 集合不完整: %v", got)
	}
}

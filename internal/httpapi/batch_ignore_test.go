package httpapi

import (
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestBatchSetWorkItemIgnored(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()
	now := time.Now().UTC()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-batch-wi",
		FullName:       "org/batch-wi",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	// Create 3 work items
	ids := []string{"wi-batch-1", "wi-batch-2", "wi-batch-3"}
	for i, id := range ids {
		_, _, err := fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
			ID:              id,
			RepositoryID:    repo.ID,
			Number:          i + 1,
			Kind:            store.WorkItemKindIssue,
			State:           "open",
			Title:           fmt.Sprintf("Issue %d", i+1),
			Author:          "dev",
			SourceUpdatedAt: now,
			StateHash:       fmt.Sprintf("hash-%d", i+1),
		}, nil)
		if err != nil {
			t.Fatalf("upsert item: %v", err)
		}
	}

	// 1. Unauthenticated request should fail with 401
	unauth := fixture.request(t, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":["wi-batch-1"],"ignored":true}`, "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 2. Empty IDs should fail with 400
	emptyReq := fixture.request(t, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":[],"ignored":true}`, "127.0.0.1:45002", cookies,
		map[string]string{CSRFHeaderName: csrf.Value})
	assertAPIError(t, emptyReq, http.StatusBadRequest, errorCodeValidationFailed)

	// 3. Batch ignore 2 items
	batchReq := fixture.request(t, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":["wi-batch-1","wi-batch-2"],"ignored":true}`, "127.0.0.1:45003", cookies,
		map[string]string{CSRFHeaderName: csrf.Value})
	if batchReq.Code != http.StatusOK {
		t.Fatalf("batch-ignore status=%d body=%s", batchReq.Code, batchReq.Body.String())
	}

	// 4. Verify in DB
	it1, err := fixture.store.WorkItems().Get(ctx, "wi-batch-1")
	if err != nil || !it1.Ignored {
		t.Fatalf("expected wi-batch-1 ignored=true, got err=%v, ignored=%v", err, it1.Ignored)
	}
	it2, err := fixture.store.WorkItems().Get(ctx, "wi-batch-2")
	if err != nil || !it2.Ignored {
		t.Fatalf("expected wi-batch-2 ignored=true, got err=%v, ignored=%v", err, it2.Ignored)
	}
	it3, err := fixture.store.WorkItems().Get(ctx, "wi-batch-3")
	if err != nil || it3.Ignored {
		t.Fatalf("expected wi-batch-3 ignored=false, got err=%v, ignored=%v", err, it3.Ignored)
	}

	// 5. Batch unignore
	unignoreReq := fixture.request(t, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":["wi-batch-1"],"ignored":false}`, "127.0.0.1:45004", cookies,
		map[string]string{CSRFHeaderName: csrf.Value})
	if unignoreReq.Code != http.StatusOK {
		t.Fatalf("batch-unignore status=%d body=%s", unignoreReq.Code, unignoreReq.Body.String())
	}
	it1, err = fixture.store.WorkItems().Get(ctx, "wi-batch-1")
	if err != nil || it1.Ignored {
		t.Fatalf("expected wi-batch-1 ignored=false, got err=%v, ignored=%v", err, it1.Ignored)
	}
}

func TestBatchSetWorkItemIgnored_ReturnsErrorForMissingID(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	ctx := t.Context()

	repo, err := fixture.store.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-batch-missing",
		FullName:       "org/batch-missing",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	_, _, err = fixture.store.WorkItems().UpsertIfNewer(ctx, store.WorkItem{
		ID:              "wi-batch-present",
		RepositoryID:    repo.ID,
		Number:          1,
		Kind:            store.WorkItemKindIssue,
		State:           "open",
		Title:           "Present issue",
		Author:          "dev",
		SourceUpdatedAt: time.Now().UTC(),
		StateHash:       "hash-present",
	}, nil)
	if err != nil {
		t.Fatalf("upsert item: %v", err)
	}

	// 合法 ID 与缺失 ID 混在同一请求：整体回滚，合法行的忽略标记不得改变。
	req := fixture.request(t, http.MethodPost, "/api/v1/work-items/batch-ignore",
		`{"ids":["wi-batch-present","missing-work-item"],"ignored":true}`, "127.0.0.1:45005", cookies,
		map[string]string{CSRFHeaderName: csrf.Value})
	if req.Code != http.StatusNotFound {
		t.Fatalf("expected missing ID to return 404, got %d: %s", req.Code, req.Body.String())
	}
	item, err := fixture.store.WorkItems().Get(ctx, "wi-batch-present")
	if err != nil {
		t.Fatalf("get item: %v", err)
	}
	if item.Ignored {
		t.Fatal("批量忽略必须整体回滚：存在缺失 ID 时不得写入任何一行")
	}
}

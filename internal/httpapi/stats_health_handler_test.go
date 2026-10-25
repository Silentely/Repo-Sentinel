package httpapi

import (
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestStatsHealthHandler(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	// 1. 未登录 -> 401 Unauthorized
	unauth := fixture.request(t, http.MethodGet, "/api/v1/stats/health", "", "127.0.0.1:45001", nil, nil)
	assertAPIError(t, unauth, http.StatusUnauthorized, "unauthorized")

	// 2. 查询不存在的 repository_id -> 404
	missingResp := fixture.request(t, http.MethodGet, "/api/v1/stats/health?repository_id=no-such-repo-id", "", "127.0.0.1:45003", cookies, nil)
	assertAPIError(t, missingResp, http.StatusNotFound, errorCodeNotFound)

	// 3. 正常查询 -> 200 OK 且包含完整字段
	now := time.Now().UTC()
	_, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID:             "health-repo-1",
		Owner:          "acme",
		Name:           "health-repo-1",
		FullName:       "acme/health-repo-1",
		DefaultBranch:  "main",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	_, _, _ = fixture.store.WorkItems().UpsertIfNewer(t.Context(), store.WorkItem{
		RepositoryID:    "health-repo-1",
		Number:          101,
		Kind:            store.WorkItemKindIssue,
		State:           "open",
		Title:           "Stagnant Issue",
		SourceUpdatedAt: now.Add(-10 * 24 * time.Hour),
		HTMLURL:         "https://github.com/acme/health-repo-1/issues/101",
	}, nil)

	resp := fixture.request(t, http.MethodGet, "/api/v1/stats/health?repository_id=health-repo-1", "", "127.0.0.1:45002", cookies, nil)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d: %s", resp.Code, resp.Body.String())
	}

	var data struct {
		Score         int    `json:"score"`
		Grade         string `json:"grade"`
		Summary       string `json:"summary"`
		StagnantItems []struct {
			Number       int `json:"number"`
			DaysStagnant int `json:"days_stagnant"`
		} `json:"stagnant_items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &data); err != nil {
		t.Fatalf("unmarshal error: %v", err)
	}

	if data.Score <= 0 || data.Score > 100 {
		t.Errorf("invalid score: %d", data.Score)
	}
	if data.Grade == "" {
		t.Errorf("missing grade")
	}
	if len(data.StagnantItems) != 1 || data.StagnantItems[0].Number != 101 {
		t.Errorf("expected 1 stagnant item #101, got %+v", data.StagnantItems)
	}
}

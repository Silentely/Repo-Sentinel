package chatops

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestExecutor_PRComment(t *testing.T) {
	st := newTestStore(t)
	ctx := t.Context()

	// Seed repository
	repo, err := st.Repositories().Upsert(ctx, store.Repository{
		ID:             "repo-pr-1",
		FullName:       "octocat/hello-world",
		MonitorEnabled: true,
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}

	var commentRequests int
	var receivedBody string
	var receivedPath string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost && strings.Contains(r.URL.Path, "/comments") {
			commentRequests++
			receivedPath = r.URL.Path
			b, _ := io.ReadAll(r.Body)
			var payload map[string]string
			_ = json.Unmarshal(b, &payload)
			receivedBody = payload["body"]
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte(`{"id": 123456}`))
			return
		}
		http.NotFound(w, r)
	}))
	defer srv.Close()

	client := &githubx.AppClient{
		HTTP:    srv.Client(),
		BaseURL: srv.URL,
	}
	executor := &GitHubChatOpsExecutor{
		Store:  st,
		Client: client,
	}

	// 1. Success execution
	action := ActionData{
		ID:          "act-pr-comment-1",
		Action:      "pr_comment",
		RepoID:      repo.ID,
		IssueNumber: 42,
		Payload:     "Looks great! Ready to merge.",
	}

	outcome, err := executor.ExecuteWithOutcome(ctx, action)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if outcome.Status != StatusExecuted {
		t.Fatalf("expected StatusExecuted, got: %s", outcome.Status)
	}
	if outcome.Message != "已成功在 PR #42 发表评论" {
		t.Fatalf("unexpected message: %s", outcome.Message)
	}
	if commentRequests != 1 {
		t.Fatalf("expected 1 comment request, got %d", commentRequests)
	}
	if receivedPath != "/repos/octocat/hello-world/issues/42/comments" {
		t.Fatalf("unexpected path: %s", receivedPath)
	}
	if receivedBody != "Looks great! Ready to merge." {
		t.Fatalf("unexpected comment body: %s", receivedBody)
	}

	// 2. Idempotency test (repeat execution with same action)
	outcome2, err2 := executor.ExecuteWithOutcome(ctx, action)
	if err2 != nil {
		t.Fatalf("unexpected error on repeated execution: %v", err2)
	}
	if outcome2.Status != StatusAlreadyDone {
		t.Fatalf("expected StatusAlreadyDone on repeated execution, got: %s", outcome2.Status)
	}
	if commentRequests != 1 {
		t.Fatalf("comment API should not be called again on duplicate action, hits: %d", commentRequests)
	}

	// 3. Validation: invalid IssueNumber <= 0
	badIssueAction := ActionData{
		ID:          "act-pr-comment-bad-num",
		Action:      "pr_comment",
		RepoID:      repo.ID,
		IssueNumber: 0,
		Payload:     "hello",
	}
	badOutcome, badErr := executor.ExecuteWithOutcome(ctx, badIssueAction)
	if badErr == nil || badOutcome.Status != StatusRetryableFailure {
		t.Fatalf("expected failure for invalid issue number, got: %+v, %v", badOutcome, badErr)
	}

	// 4. Validation: empty payload
	emptyPayloadAction := ActionData{
		ID:          "act-pr-comment-empty-body",
		Action:      "pr_comment",
		RepoID:      repo.ID,
		IssueNumber: 42,
		Payload:     "   \n  \t ",
	}
	emptyOutcome, emptyErr := executor.ExecuteWithOutcome(ctx, emptyPayloadAction)
	if emptyErr == nil || emptyOutcome.Status != StatusRetryableFailure {
		t.Fatalf("expected failure for empty payload, got: %+v, %v", emptyOutcome, emptyErr)
	}

	// 5. Payload trimming if > 2000 chars
	longPayload := strings.Repeat("A", 2500)
	longAction := ActionData{
		ID:          "act-pr-comment-long",
		Action:      "pr_comment",
		RepoID:      repo.ID,
		IssueNumber: 42,
		Payload:     longPayload,
	}
	longOutcome, longErr := executor.ExecuteWithOutcome(ctx, longAction)
	if longErr != nil {
		t.Fatalf("unexpected error for long payload: %v", longErr)
	}
	if longOutcome.Status != StatusExecuted {
		t.Fatalf("expected StatusExecuted for trimmed payload, got: %s", longOutcome.Status)
	}
	if len(receivedBody) != 2000 {
		t.Fatalf("expected received body to be trimmed to 2000 chars, got length: %d", len(receivedBody))
	}
}

func TestGuardrail_PRCommentRisk(t *testing.T) {
	risk, ok := ActionRisk("pr_comment")
	if !ok || risk != "medium" {
		t.Fatalf("expected ActionRisk(pr_comment) == (medium, true), got: (%s, %v)", risk, ok)
	}
}

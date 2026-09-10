package ai

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestReviewPRAndFormatComment(t *testing.T) {
	fakeServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{
			"choices": [
				{
					"message": {
						"content": "{\"summary\": \"测试审查通过\", \"score\": 95, \"security_risks\": [\"潜在的敏感配置\"], \"breaking_risks\": [], \"code_smells\": [\"建议增加单元测试\"]}"
					}
				}
			]
		}`))
	}))
	defer fakeServer.Close()

	client := &Client{
		BaseURL: fakeServer.URL,
		Model:   "test-model",
		APIKey:  "test-key",
		Enabled: true,
	}

	res, err := client.ReviewPR(context.Background(), "test/repo", "Fix bug in webhook", "alice", "+ func Fix() {}")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if res.Summary != "测试审查通过" {
		t.Errorf("expected summary, got %s", res.Summary)
	}
	if res.Score != 95 {
		t.Errorf("expected score 95, got %d", res.Score)
	}
	if len(res.SecurityRisks) != 1 || res.SecurityRisks[0] != "潜在的敏感配置" {
		t.Errorf("unexpected security risks: %v", res.SecurityRisks)
	}
	if len(res.CodeSmells) != 1 || res.CodeSmells[0] != "建议增加单元测试" {
		t.Errorf("unexpected code smells: %v", res.CodeSmells)
	}

	comment := FormatPRComment(res)
	if !strings.Contains(comment, "## 🤖 RepoSentinel AI Code Review") {
		t.Errorf("comment missing title: %s", comment)
	}
	if !strings.Contains(comment, "95 / 100") {
		t.Errorf("comment missing score: %s", comment)
	}
	if !strings.Contains(comment, "潜在的敏感配置") {
		t.Errorf("comment missing security risk: %s", comment)
	}
}

func TestReviewPRRejectsMalformedResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"not json"}}]}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "test-key", Enabled: true}
	res, err := client.ReviewPR(context.Background(), "o/r", "title", "author", "diff")
	if err == nil || res != nil {
		t.Fatalf("格式错误的审查响应应返回错误，res=%+v err=%v", res, err)
	}
}

func TestReviewPRMarksTruncatedDiff(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"summary\":\"ok\",\"score\":90}"}}]}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "test-key", Enabled: true}
	res, err := client.ReviewPR(context.Background(), "o/r", "title", "author", strings.Repeat("x", maxPRDiffChars+1))
	if err != nil {
		t.Fatal(err)
	}
	if !res.DiffTruncated {
		t.Fatal("超过输入上限的审查结果必须标记 DiffTruncated")
	}
}

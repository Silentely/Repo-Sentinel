package ai

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestTriageIssue(t *testing.T) {
	t.Run("Disabled", func(t *testing.T) {
		client := &Client{Enabled: false}
		_, err := client.TriageIssue(context.Background(), "org/repo", "Issue title", "author", "body")
		if err == nil {
			t.Fatal("expected error when disabled")
		}
	})

	t.Run("Success", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			resp := `{
				"choices": [{
					"message": {
						"content": "{\"category\":\"Bug Report\",\"priority\":\"P1 High\",\"summary\":\"登录界面在特定分辨率下白屏\",\"missing_details\":[\"操作系统版本\",\"控制台报错日志\"],\"suggested_reply\":\"你好，感谢反馈！为了快速定位白屏问题，能否提供一下具体的浏览器版本与控制台报错日志？\",\"confidence\":5}"
					}
				}]
			}`
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(resp))
		}))
		defer ts.Close()

		client := &Client{
			Enabled:       true,
			APIKey:        "test-key",
			BaseURL:       ts.URL,
			TriageEnabled: true,
		}

		res, err := client.TriageIssue(context.Background(), "org/repo", "登录白屏", "user1", "打开后白屏，无法使用")
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Category != "Bug Report" {
			t.Errorf("expected category Bug Report, got %s", res.Category)
		}
		if res.Priority != "P1 High" {
			t.Errorf("expected priority P1 High, got %s", res.Priority)
		}
		if len(res.MissingDetails) != 2 {
			t.Fatalf("expected 2 missing details, got %d", len(res.MissingDetails))
		}
		if res.Confidence != 5 {
			t.Errorf("expected confidence 5, got %d", res.Confidence)
		}

		formatted := FormatIssueTriage(res)
		if !strings.Contains(formatted, "🐛 类别：Bug Report") {
			t.Errorf("expected formatted to contain category, got: %s", formatted)
		}
		if !strings.Contains(formatted, "缺失要素：操作系统版本、控制台报错日志") {
			t.Errorf("expected formatted to contain missing details, got: %s", formatted)
		}
		if !strings.Contains(formatted, "💬 建议首响回复：") {
			t.Errorf("expected formatted to contain suggested reply, got: %s", formatted)
		}
	})
}

func TestTriageIssueRobustnessAndFallbacks(t *testing.T) {
	t.Run("MarkdownCodeFenceAndFallbacks", func(t *testing.T) {
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			resp := "```json\n{\"summary\":\"用户建议增加深色模式\",\"suggested_reply\":\"感谢建议，已纳入规划\"}\n```"
			data, _ := json.Marshal(map[string]any{
				"choices": []any{
					map[string]any{"message": map[string]any{"content": resp}},
				},
			})
			_, _ = w.Write(data)
		}))
		defer ts.Close()

		client := &Client{Enabled: true, APIKey: "test-key", BaseURL: ts.URL, TriageEnabled: true}
		if _, err := client.TriageIssue(context.Background(), "org/repo", "Feature", "user", "Dark mode please"); err == nil {
			t.Fatal("expected incomplete model response to be rejected")
		}
	})

	t.Run("RejectsInvalidEnumsAndRequiredFields", func(t *testing.T) {
		cases := []string{
			`{"category":"Other","priority":"P2 Normal","summary":"摘要","suggested_reply":"回复","confidence":4}`,
			`{"category":"Question","priority":"P9 Unknown","summary":"摘要","suggested_reply":"回复","confidence":4}`,
			`{"category":"Question","priority":"P2 Normal","summary":"","suggested_reply":"回复","confidence":4}`,
			`{"category":"Question","priority":"P2 Normal","summary":"摘要","suggested_reply":"","confidence":4}`,
			`{"category":"Question","priority":"P2 Normal","summary":"摘要","suggested_reply":"回复","confidence":6}`,
		}
		for _, content := range cases {
			t.Run(content, func(t *testing.T) {
				ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
					w.Header().Set("Content-Type", "application/json")
					data, _ := json.Marshal(map[string]any{"choices": []any{map[string]any{
						"message": map[string]any{"content": content},
					}}})
					_, _ = w.Write(data)
				}))
				defer ts.Close()
				client := &Client{Enabled: true, APIKey: "test-key", BaseURL: ts.URL, TriageEnabled: true}
				if _, err := client.TriageIssue(context.Background(), "org/repo", "Issue", "user", "body"); err == nil {
					t.Fatal("expected invalid triage response to be rejected")
				}
			})
		}
	})

	t.Run("LongBodyTruncation", func(t *testing.T) {
		var receivedPrompt string
		ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rawBody, _ := io.ReadAll(r.Body)
			receivedPrompt = string(rawBody)
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{\"category\":\"Bug Report\",\"priority\":\"P0 Blocker\",\"summary\":\"超长堆栈\",\"suggested_reply\":\"排查中\",\"confidence\":5}"}}]}`))
		}))
		defer ts.Close()

		client := &Client{Enabled: true, APIKey: "test-key", BaseURL: ts.URL, TriageEnabled: true}
		hugeBody := strings.Repeat("Error at line 1000\n", 1000)
		res, err := client.TriageIssue(context.Background(), "org/repo", "Crash", "user", hugeBody)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if res.Priority != "P0 Blocker" {
			t.Errorf("expected P0 Blocker, got %s", res.Priority)
		}
		if !strings.Contains(receivedPrompt, "正文已截断") {
			t.Fatalf("expected prompt to indicate truncation")
		}
	})
}

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

func TestTriageLabelMappingsAndDerivation(t *testing.T) {
	// 1. 默认标签映射完整性检查
	defaults := DefaultTriageLabelMappings()
	expectedKeys := []string{
		"Bug Report", "Feature Request", "Question", "Incomplete", "Invalid",
		"P0 Blocker", "P1 High", "P2 Normal", "P3 Low",
	}
	for _, key := range expectedKeys {
		if _, ok := defaults[key]; !ok {
			t.Errorf("DefaultTriageLabelMappings missing key %q", key)
		}
	}

	// 2. 默认标签推导
	res := &IssueTriageResult{
		Category: "Bug Report",
		Priority: "P1 High",
	}
	labels := DeriveTriageLabels(res, nil)
	if len(labels) != 2 || labels[0] != "bug" || labels[1] != "priority: high" {
		t.Errorf("DeriveTriageLabels(res, nil) = %v, want [bug, priority: high]", labels)
	}

	// 3. 自定义映射覆盖测试
	custom := map[string]string{
		"Bug Report": "type:defect",
		"P1 High":    "severity:urgent",
	}
	customLabels := DeriveTriageLabels(res, custom)
	if len(customLabels) != 2 || customLabels[0] != "type:defect" || customLabels[1] != "severity:urgent" {
		t.Errorf("DeriveTriageLabels(res, custom) = %v, want [type:defect, severity:urgent]", customLabels)
	}

	// 4. 卡片格式化输出包含建议标签
	res.Labels = customLabels
	res.Summary = "登录无响应"
	formatted := FormatIssueTriage(res)
	if !strings.Contains(formatted, "🏷️ 建议标签：type:defect, severity:urgent") {
		t.Errorf("FormatIssueTriage missing labels line, got: %s", formatted)
	}

	// 5. Client 运行时继承与快照测试
	client := &Client{
		Enabled:             true,
		APIKey:              "key",
		TriageEnabled:       true,
		TriageLabelMappings: custom,
	}
	snap := client.Snapshot()
	if snap.TriageLabelMappings["Bug Report"] != "type:defect" {
		t.Errorf("Snapshot failed to copy TriageLabelMappings")
	}

	// 修改快照不得影响原始 client
	snap.TriageLabelMappings["Bug Report"] = "changed"
	if client.TriageMappings()["Bug Report"] != "type:defect" {
		t.Errorf("Snapshot mutation mutated original client mappings")
	}
}


func TestPersonalMaintainerTriageOptimizations(t *testing.T) {
	// 1. deriveSuspectedModule 模块推导
	if mod := deriveSuspectedModule("Webhook payload timeout", "delivery error"); !strings.Contains(mod, "webhooksvc") {
		t.Errorf("expected webhooksvc module, got %s", mod)
	}
	if mod := deriveSuspectedModule("规则引擎在静音期不生效", "quiet hour bug"); !strings.Contains(mod, "rules") {
		t.Errorf("expected rules module, got %s", mod)
	}
	if mod := deriveSuspectedModule("Web 前端卡片折叠按钮无响应", "ui click failed"); !strings.Contains(mod, "web/src") {
		t.Errorf("expected web/src module, got %s", mod)
	}

	// 2. deriveTriageMaintainerAction 针对外部用户 vs 个人维护者自建
	resBug := &IssueTriageResult{Category: "Bug Report", Priority: "P1 High"}
	if action := deriveTriageMaintainerAction(resBug, false); !strings.Contains(action, "高危缺陷") {
		t.Errorf("unexpected action for P1 bug: %s", action)
	}
	if action := deriveTriageMaintainerAction(resBug, true); !strings.Contains(action, "自建待修") {
		t.Errorf("unexpected action for self bug: %s", action)
	}

	resInvalid := &IssueTriageResult{Category: "Invalid", Priority: "P3 Low"}
	if action := deriveTriageMaintainerAction(resInvalid, false); !strings.Contains(action, "垃圾内容：建议直接关闭") {
		t.Errorf("unexpected action for invalid: %s", action)
	}

	// 3. FormatIssueTriage 包含维护者建议与模块
	resBug.MaintainerAction = "高危缺陷：已确认为高优先级 Bug，建议优先跟进修复"
	resBug.SuspectedModule = "internal/webhooksvc (Webhook 接收与处理)"
	resBug.Summary = "高并发下 Webhook 投递超时"
	formatted := FormatIssueTriage(resBug)

	if !strings.Contains(formatted, "🎯 建议处理：高危缺陷") {
		t.Errorf("FormatIssueTriage missing 建议处理, got: %s", formatted)
	}
	if !strings.Contains(formatted, "🧩 疑似模块：internal/webhooksvc") {
		t.Errorf("FormatIssueTriage missing 疑似模块, got: %s", formatted)
	}
}

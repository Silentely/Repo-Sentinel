package rules

import (
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestRenderMessageIssueOpened(t *testing.T) {
	num := 42
	ev := &store.Event{
		Kind:          store.WorkItemKindIssue,
		Action:        "opened",
		Title:         "修复登录 Bug",
		Actor:         "dev-user",
		Severity:      "high",
		SubjectNumber: &num,
		HTMLURL:       "https://github.com/org/repo/issues/42",
	}
	title, body, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "🐛") {
		t.Fatalf("标题应包含 bug emoji，实际: %s", title)
	}
	if !strings.Contains(title, "修复登录 Bug") {
		t.Fatalf("标题应包含事件标题，实际: %s", title)
	}
	if !strings.Contains(body, "org/repo") {
		t.Fatal("正文应包含仓库名")
	}
	if !strings.Contains(body, "dev-user") {
		t.Fatal("正文应包含操作者")
	}
	if !strings.Contains(body, "high") {
		t.Fatal("正文应包含严重度")
	}
	if !strings.Contains(body, "#42") {
		t.Fatal("正文应包含编号")
	}
	if !strings.Contains(body, "🔗 在 GitHub 中查看") {
		t.Fatal("正文应包含 GitHub 链接按钮文字")
	}
	if htmlURL != "https://github.com/org/repo/issues/42" {
		t.Fatalf("htmlURL 应为 GitHub 链接，实际: %s", htmlURL)
	}
}

func TestRenderMessagePRMerged(t *testing.T) {
	ev := &store.Event{
		Kind:    store.WorkItemKindPR,
		Action:  "merged",
		Title:   "合并功能分支",
		Actor:   "maintainer",
		HTMLURL: "https://github.com/org/repo/pull/10",
	}
	title, body, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "🟣") {
		t.Fatalf("merged 应显示 🟣，实际: %s", title)
	}
	if !strings.Contains(body, "pull_request") {
		t.Fatal("正文应包含 PR 类型")
	}
	if htmlURL != "https://github.com/org/repo/pull/10" {
		t.Fatalf("htmlURL 不正确: %s", htmlURL)
	}
}

func TestRenderMessageWorkflowFailed(t *testing.T) {
	ev := &store.Event{
		Kind:               "workflow_run",
		Action:             "completed",
		WorkflowConclusion: "failure",
		Title:              "CI 构建失败",
		HTMLURL:            "https://github.com/org/repo/actions/runs/99",
	}
	title, _, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "❌") {
		t.Fatalf("workflow failure 应显示 ❌，实际: %s", title)
	}
	if htmlURL != "https://github.com/org/repo/actions/runs/99" {
		t.Fatalf("htmlURL 不正确: %s", htmlURL)
	}
}

func TestRenderMessageNoURLReturnsEmptyHTMLURL(t *testing.T) {
	ev := &store.Event{
		Kind:   store.AlertKindDependabot,
		Action: "created",
		Title:  "依赖漏洞",
	}
	title, body, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "📦") {
		t.Fatalf("dependabot 应显示 📦，实际: %s", title)
	}
	if htmlURL != "" {
		t.Fatalf("无 HTMLURL 时应返回空字符串，实际: %s", htmlURL)
	}
	if strings.Contains(body, "🔗 在 GitHub 中查看") {
		t.Fatal("无 URL 时不应包含 GitHub 链接文字")
	}
}

func TestRenderMessageSeparatorPresent(t *testing.T) {
	ev := &store.Event{
		Kind:    store.WorkItemKindIssue,
		Action:  "opened",
		Title:   "测试",
		HTMLURL: "https://example.com",
	}
	_, body, _ := renderMessage(ev, "repo")
	if !strings.Contains(body, "────────────────") {
		t.Fatal("正文应包含分隔线")
	}
}

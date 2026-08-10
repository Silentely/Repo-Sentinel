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

	if !strings.Contains(title, "🟢") {
		t.Fatalf("已打开标题应含 🟢，实际: %s", title)
	}
	if !strings.Contains(title, "已打开") {
		t.Fatalf("标题应一眼可读「已打开」，实际: %s", title)
	}
	if !strings.Contains(title, "修复登录 Bug") {
		t.Fatalf("标题应包含事件标题，实际: %s", title)
	}
	if !strings.Contains(body, "状态：已打开") {
		t.Fatalf("正文应突出状态，实际: %s", body)
	}
	if !strings.Contains(body, "org/repo") {
		t.Fatal("正文应包含仓库名")
	}
	if !strings.Contains(body, "dev-user") {
		t.Fatal("正文应包含操作者")
	}
	if !strings.Contains(body, "高 (high)") {
		t.Fatal("正文应包含中文严重度")
	}
	if !strings.Contains(body, "#42") {
		t.Fatal("正文应包含编号")
	}
	if strings.Contains(body, "pull_request /") || strings.Contains(body, "issue / opened") {
		t.Fatal("不应再使用 raw kind/action 拼接")
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
	if !strings.Contains(title, "已合并") {
		t.Fatalf("标题应含「已合并」，实际: %s", title)
	}
	if !strings.Contains(body, "状态：已合并") {
		t.Fatalf("正文应含状态：已合并，实际: %s", body)
	}
	if !strings.Contains(body, "类型：PR") {
		t.Fatal("正文应包含友好类型 PR")
	}
	if htmlURL != "https://github.com/org/repo/pull/10" {
		t.Fatalf("htmlURL 不正确: %s", htmlURL)
	}
}

func TestRenderMessageIssueClosed(t *testing.T) {
	num := 7
	ev := &store.Event{
		Kind:          store.WorkItemKindIssue,
		Action:        "closed",
		Title:         "AI模组保存KEY似乎不会写入",
		SubjectNumber: &num,
		HTMLURL:       "https://github.com/org/repo/issues/7",
	}
	title, body, _ := renderMessage(ev, "org/repo")
	if !strings.Contains(title, "已关闭") {
		t.Fatalf("关闭 issue 标题应含「已关闭」，实际: %s", title)
	}
	if !strings.Contains(body, "状态：已关闭") {
		t.Fatalf("正文应含「已关闭」，实际: %s", body)
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
	title, body, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "❌") {
		t.Fatalf("workflow failure 应显示 ❌，实际: %s", title)
	}
	if !strings.Contains(title, "失败") {
		t.Fatalf("标题应含「失败」，实际: %s", title)
	}
	if !strings.Contains(body, "状态：失败") {
		t.Fatalf("正文应含状态：失败，实际: %s", body)
	}
	if htmlURL != "https://github.com/org/repo/actions/runs/99" {
		t.Fatalf("htmlURL 不正确: %s", htmlURL)
	}
}

func TestRenderMessageNoURLReturnsEmptyHTMLURL(t *testing.T) {
	ev := &store.Event{
		Kind:     store.AlertKindDependabot,
		Action:   "created",
		Title:    "依赖漏洞",
		Severity: "medium",
	}
	title, body, htmlURL := renderMessage(ev, "org/repo")

	if !strings.Contains(title, "新告警") {
		t.Fatalf("dependabot 创建应显示「新告警」，实际: %s", title)
	}
	if !strings.Contains(body, "中 (medium)") {
		t.Fatalf("严重度应中文化，实际: %s", body)
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

func TestRenderMessageReleasePublished(t *testing.T) {
	releaseID := 42
	ev := &store.Event{
		Kind:          store.ReleaseKind,
		Action:        "published",
		Title:         "Hello-World v2.0.0",
		SubjectNumber: &releaseID,
		HTMLURL:       "https://github.com/octocat/Hello-World/releases/tag/v2.0.0",
		PayloadSummary: map[string]any{
			"tag_name": "v2.0.0", "prerelease": false, "notes": "some notes",
		},
	}
	title, body, htmlURL := renderMessage(ev, "octocat/Hello-World")

	if !strings.Contains(title, "🚀") {
		t.Fatalf("release 标题应含 🚀，实际: %s", title)
	}
	if !strings.Contains(title, "新版本发布") {
		t.Fatalf("标题应含「新版本发布」，实际: %s", title)
	}
	if !strings.Contains(body, "状态：新版本发布") {
		t.Fatalf("正文应含状态：新版本发布，实际: %s", body)
	}
	if !strings.Contains(body, "版本：<code>v2.0.0</code>") {
		t.Fatalf("正文应含版本 tag，实际: %s", body)
	}
	if !strings.Contains(body, "类型：Release") {
		t.Fatalf("正文应含类型 Release，实际: %s", body)
	}
	// release 事件不应渲染「编号」行（编号是 release id 而非 issue 编号）。
	if strings.Contains(body, "编号：") {
		t.Fatalf("release 正文不应含编号行，实际: %s", body)
	}
	if htmlURL != "https://github.com/octocat/Hello-World/releases/tag/v2.0.0" {
		t.Fatalf("htmlURL 不正确: %s", htmlURL)
	}
}

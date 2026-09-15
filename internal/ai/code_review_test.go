package ai

import (
	"context"
	"errors"
	"fmt"
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
	// 防幻觉保底拦截：存在 security_risks 时，95 分必须被算法层安全截断至 50 分
	if res.Score != 50 {
		t.Errorf("expected score 50 (capped from 95 by security guard), got %d", res.Score)
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
	if !strings.Contains(comment, "50 / 100") {
		t.Errorf("comment missing score: %s", comment)
	}
	if !strings.Contains(comment, "潜在的敏感配置") {
		t.Errorf("comment missing security risk: %s", comment)
	}
	if !strings.Contains(comment, "安全风险警示") {
		t.Errorf("comment missing security alert banner: %s", comment)
	}
}

func TestFormatPRCommentRiskBannerAndBadges(t *testing.T) {
	// Case 1: 无风险高分 PR
	cleanRes := &CodeReviewResult{
		Summary: "功能实现干净完整",
		Score:   92,
	}
	cleanComment := FormatPRComment(cleanRes)
	if strings.Contains(cleanComment, "安全风险警示") {
		t.Errorf("clean pr should not have security banner: %s", cleanComment)
	}
	if !strings.Contains(cleanComment, "🟢 健康") {
		t.Errorf("clean pr should have green health badge: %s", cleanComment)
	}

	// Case 2: 存在安全风险（如不可信外链或钓鱼嫌疑）
	riskRes := &CodeReviewResult{
		Summary:       "疑似恶意外链",
		Score:         35,
		SecurityRisks: []string{"文档包含不可信外链 pages.dev 疑似钓鱼"},
	}
	riskComment := FormatPRComment(riskRes)
	if !strings.Contains(riskComment, "🚨 **安全风险警示**") {
		t.Errorf("risky pr missing alert banner: %s", riskComment)
	}
	if !strings.Contains(riskComment, "🔴 高危风险") {
		t.Errorf("risky pr missing danger badge: %s", riskComment)
	}

	// Case 3: 仅评分低（无明确 security_risks，如大面积破坏性变更）
	lowScoreRes := &CodeReviewResult{
		Summary:       "破坏性过大",
		Score:         45,
		BreakingRisks: []string{"删除核心导出函数"},
	}
	lowScoreComment := FormatPRComment(lowScoreRes)
	if !strings.Contains(lowScoreComment, "⚠️ **质量风险警示**") {
		t.Errorf("low score pr missing quality banner: %s", lowScoreComment)
	}
	if !strings.Contains(lowScoreComment, "🔴 高危风险") {
		t.Errorf("low score pr missing danger badge: %s", lowScoreComment)
	}
}

func TestCleanAndPrioritizeDiff(t *testing.T) {
	// 构造包含超长 lockfile、高危 workflow 以及业务代码的 Diff
	var lockLines []string
	lockLines = append(lockLines, "diff --git a/package-lock.json b/package-lock.json", "--- a/package-lock.json", "+++ b/package-lock.json", "@@ -1,5 +1,500 @@")
	for i := 0; i < 500; i++ {
		lockLines = append(lockLines, fmt.Sprintf("+   \"version\": \"1.0.%d\",", i))
	}
	rawLockDiff := strings.Join(lockLines, "\n")

	rawWorkflowDiff := `diff --git a/.github/workflows/deploy.yml b/.github/workflows/deploy.yml
--- a/.github/workflows/deploy.yml
+++ b/.github/workflows/deploy.yml
@@ -1,3 +1,5 @@
+on:
+  pull_request_target:
+jobs:
+  run:
+    runs-on: ubuntu-latest`

	rawCodeDiff := `diff --git a/pkg/service.go b/pkg/service.go
--- a/pkg/service.go
+++ b/pkg/service.go
@@ -1,2 +1,3 @@
+func Process() error { return nil }`

	combined := rawLockDiff + "\n" + rawCodeDiff + "\n" + rawWorkflowDiff

	cleaned, truncated := cleanAndPrioritizeDiff(combined, 1000)
	if truncated {
		t.Fatalf("cleaned diff should not be truncated because lockfile is summarized")
	}

	// 验证 lockfile 被自动精简
	if !strings.Contains(cleaned, "依赖锁定文件/构建产物变更已自动精简") {
		t.Errorf("expected lockfile to be summarized, got: %s", cleaned)
	}
	// 验证优先级调度：高优先级的 .github/workflows/ 必须排在最前部
	workflowIdx := strings.Index(cleaned, ".github/workflows/deploy.yml")
	codeIdx := strings.Index(cleaned, "pkg/service.go")
	lockIdx := strings.Index(cleaned, "package-lock.json")

	if workflowIdx == -1 || codeIdx == -1 || lockIdx == -1 {
		t.Fatalf("all files should be present in cleaned diff: %s", cleaned)
	}
	if !(workflowIdx < codeIdx && codeIdx < lockIdx) {
		t.Errorf("expected order: workflow < code < lockfile, got workflow=%d code=%d lock=%d", workflowIdx, codeIdx, lockIdx)
	}
}

func TestScanDiffHeuristics(t *testing.T) {
	diff := `diff --git a/.github/workflows/pr.yml b/.github/workflows/pr.yml
--- a/.github/workflows/pr.yml
+++ b/.github/workflows/pr.yml
@@ -1,4 +1,14 @@
+on:
+  pull_request_target:
+jobs:
+  test:
+    steps:
+      - uses: actions/checkout@v4
+        with:
+          ref: ${{ github.event.pull_request.head.sha }}
+      - run: |
+          echo "Testing PR"
+          echo "${{ github.event.issue.title }}"
diff --git a/docs/0123456789abcdef0123.md b/docs/0123456789abcdef0123.md
--- /dev/null
+++ b/docs/0123456789abcdef0123.md
@@ -0,0 +1,3 @@
+# Info
+Check this site: https://malicious-page.pages.dev/login
+Or shortlink: https://bit.ly/secure-offer`

	report := scanDiffHeuristics(diff)
	if len(report.flaggedLinks) < 2 {
		t.Errorf("expected at least 2 flagged links, got %v", report.flaggedLinks)
	}
	if len(report.flaggedWorkflows) < 2 {
		t.Errorf("expected 2 workflow risks, got %v", report.flaggedWorkflows)
	}
	if len(report.flaggedSpamDocs) < 1 {
		t.Errorf("expected at least 1 spam doc flagged, got %v", report.flaggedSpamDocs)
	}
	if len(report.hints) < 3 {
		t.Errorf("expected hints generated for links, workflows, spam docs, got %v", report.hints)
	}
}

func TestScanDiffHeuristicsSafePRTarget(t *testing.T) {
	diff := `diff --git a/.github/workflows/pr.yml b/.github/workflows/pr.yml
--- a/.github/workflows/pr.yml
+++ b/.github/workflows/pr.yml
@@ -1,4 +1,6 @@
+on:
+  pull_request_target:
+jobs:
+  label:
+    steps:
+      - uses: actions/labeler@v5`

	report := scanDiffHeuristics(diff)
	if len(report.flaggedWorkflows) > 0 {
		t.Errorf("safe pull_request_target should not flag hard workflow risks: %v", report.flaggedWorkflows)
	}
	if len(report.hints) == 0 {
		t.Errorf("safe pull_request_target should still provide reviewer hint")
	}
}

func TestScanDiffHeuristicsMultiWorkflowIsolation(t *testing.T) {
	// 文件 A 仅含有 pull_request_target
	// 文件 B 仅含有普通的 pull_request + checkout ref，两个文件应完全隔离判定
	diff := `diff --git a/.github/workflows/labeler.yml b/.github/workflows/labeler.yml
--- a/.github/workflows/labeler.yml
+++ b/.github/workflows/labeler.yml
@@ -1,4 +1,6 @@
+on:
+  pull_request_target:
+jobs:
+  label:
+    steps:
+      - uses: actions/labeler@v5
diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1,4 +1,8 @@
+on:
+  pull_request:
+jobs:
+  test:
+    steps:
+      - uses: actions/checkout@v4
+        with:
+          ref: ${{ github.event.pull_request.head.sha }}`

	report := scanDiffHeuristics(diff)
	if len(report.flaggedWorkflows) > 0 {
		t.Errorf("cross-file workflows should be isolated and not trigger combined risk: %v", report.flaggedWorkflows)
	}
}

func TestApplyScoreAndRiskGuards(t *testing.T) {
	// Case 1: 模型产生幻觉给 90 分，但识别了钓鱼垃圾风险
	res := &CodeReviewResult{
		Summary:       "疑似钓鱼宣传",
		Score:         90,
		SecurityRisks: []string{"文档包含不可信外链 pages.dev 疑似钓鱼"},
	}
	applyScoreAndRiskGuards(res, heuristicReport{})
	if res.Score != 35 {
		t.Errorf("expected score capped to 35 for phishing risk, got %d", res.Score)
	}

	// Case 2: 模型遗漏了启发式嗅探出的外链风险，且给出了 95 分
	resMissed := &CodeReviewResult{
		Summary: "文档更新",
		Score:   95,
	}
	rep := heuristicReport{
		flaggedLinks: []string{"https://evil.pages.dev/token"},
	}
	applyScoreAndRiskGuards(resMissed, rep)
	if len(resMissed.SecurityRisks) == 0 {
		t.Errorf("expected heuristic link fallback injected into security risks")
	}
	if resMissed.Score > 35 {
		t.Errorf("expected score capped to <= 35, got %d", resMissed.Score)
	}

	// Case 3: 破坏性变更分数保底
	resBreaking := &CodeReviewResult{
		Summary:       "移除废弃接口",
		Score:         88,
		BreakingRisks: []string{"删除公共方法"},
	}
	applyScoreAndRiskGuards(resBreaking, heuristicReport{})
	if resBreaking.Score != 75 {
		t.Errorf("expected score capped to 75 for breaking changes, got %d", resBreaking.Score)
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

func TestReviewPRRejectsEmptyResponse(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	defer server.Close()

	client := &Client{BaseURL: server.URL, APIKey: "test-key", Enabled: true}
	res, err := client.ReviewPR(context.Background(), "o/r", "title", "author", "diff")
	if err == nil || res != nil {
		t.Fatalf("空审查响应应返回错误，res=%+v err=%v", res, err)
	}
	if !errors.Is(err, ErrInvalidCodeReview) {
		t.Fatalf("expected ErrInvalidCodeReview, got %v", err)
	}
}

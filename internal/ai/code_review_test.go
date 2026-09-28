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

func TestCleanAndPrioritizeDiffCRLFAndSeparation(t *testing.T) {
	crlfDiff := "diff --git a/a.go b/a.go\r\n--- a/a.go\r\n+++ b/a.go\r\n@@ -1 +1 @@\r\n-old\r\n+new\r\ndiff --git a/b.go b/b.go\r\n--- a/b.go\r\n+++ b/b.go\r\n@@ -1 +1 @@\r\n-foo\r\n+bar"
	cleaned, truncated := cleanAndPrioritizeDiff(crlfDiff, 2000)
	if truncated {
		t.Fatal("should not be truncated")
	}
	if strings.Contains(cleaned, "\r") {
		t.Fatal("cleaned diff should not contain carriage returns")
	}
	if !strings.Contains(cleaned, "\ndiff --git a/b.go b/b.go") {
		t.Fatalf("expected second diff header to start on new line, got:\n%s", cleaned)
	}
}

func TestScanDiffHeuristicsPrototypePollutionAndTestAwareness(t *testing.T) {
	// 模拟类似 PR #112 的场景：修改 edge 代理函数，加入原型污染黑名单，且未提供测试文件
	diff := `diff --git a/netlify/edge-functions/bff-proxy.js b/netlify/edge-functions/bff-proxy.js
--- a/netlify/edge-functions/bff-proxy.js
+++ b/netlify/edge-functions/bff-proxy.js
@@ -1,3 +1,5 @@
+const dangerousKeys = ['__proto__', 'constructor', 'prototype'];
+if (Object.keys(body).some(k => dangerousKeys.includes(k))) throw new Error('invalid');`

	report := scanDiffHeuristics(diff)
	if !report.hasProdCode {
		t.Errorf("expected hasProdCode to be true")
	}
	if report.hasTests {
		t.Errorf("expected hasTests to be false")
	}
	if !report.hasEdgeOrProxy {
		t.Errorf("expected hasEdgeOrProxy to be true for netlify/edge-functions/")
	}

	hasBlacklistHint := false
	hasTestHint := false
	hasEdgeHint := false
	for _, h := range report.hints {
		if strings.Contains(h, "原型污染") || strings.Contains(h, "黑名单") {
			hasBlacklistHint = true
		}
		if strings.Contains(h, "测试覆盖警示") {
			hasTestHint = true
		}
		if strings.Contains(h, "跨环境一致性提示") {
			hasEdgeHint = true
		}
	}
	if !hasBlacklistHint {
		t.Errorf("expected prototype pollution blacklist hint, got %v", report.hints)
	}
	if !hasTestHint {
		t.Errorf("expected test coverage warning hint, got %v", report.hints)
	}
	if !hasEdgeHint {
		t.Errorf("expected edge/proxy environment hint, got %v", report.hints)
	}
}

func TestApplyScoreAndRiskGuardsMetadataAndMissingTests(t *testing.T) {
	res := &CodeReviewResult{
		Summary:      "安全加固",
		Score:        95,
		Confidence:   0, // 未设置，应自动修正
		Category:     "",
		MergeRisk:    "",
		MissingTests: []string{"缺少针对 __proto__ 键名的 400 校验测试用例"},
	}
	rep := heuristicReport{
		hasProdCode: true,
		hasTests:    false,
	}

	applyScoreAndRiskGuards(res, rep)

	// 存在 MissingTests 时，高分 95 必须被保底拦截至 85
	if res.Score != 85 {
		t.Errorf("expected score capped to 85 when missing tests, got %d", res.Score)
	}
	// 生产代码无测试时置信度上限为 4
	if res.Confidence > 4 || res.Confidence < 1 {
		t.Errorf("expected confidence <= 4 and >= 1, got %d", res.Confidence)
	}
	// 合并风险应为 Low
	if res.MergeRisk != "Low" {
		t.Errorf("expected MergeRisk 'Low', got %s", res.MergeRisk)
	}

	// 测试 Diff 截断时置信度强制降至 <= 3
	res.DiffTruncated = true
	res.Confidence = 5
	applyScoreAndRiskGuards(res, rep)
	if res.Confidence > 3 {
		t.Errorf("expected confidence capped to <= 3 when truncated, got %d", res.Confidence)
	}

	// 测试高危安全风险时合并风险升级为 High 或 Critical
	resRisky := &CodeReviewResult{
		Summary:       "发现漏洞",
		Score:         30,
		SecurityRisks: []string{"严重注入漏洞"},
	}
	applyScoreAndRiskGuards(resRisky, rep)
	if resRisky.MergeRisk != "Critical" && resRisky.MergeRisk != "High" {
		t.Errorf("expected MergeRisk High or Critical for security risks, got %s", resRisky.MergeRisk)
	}
}

func TestFormatPRCommentRichStructureAndSuggestions(t *testing.T) {
	res := &CodeReviewResult{
		Summary:    "对网关代理进行安全加固",
		Score:      85,
		Confidence: 4,
		Category:   "Security Fix",
		MergeRisk:  "Low",
		MissingTests: []string{
			"缺少针对嵌套非法 prototype 属性时的拦截测试",
		},
		Suggestions: []ReviewSuggestion{
			{
				Title:         "建议改用字段白名单以防绕过",
				FilePath:      "netlify/edge-functions/bff-proxy.js",
				Description:   "黑名单防御容易被嵌套对象逃逸，建议按需白名单解构",
				SuggestedCode: "```javascript\nconst allowed = new Set(['data', 'size']);\n```",
			},
		},
	}

	comment := FormatPRComment(res)

	// 检查元数据表格
	if !strings.Contains(comment, "| 代码健康评分 | 审查置信度 | 变更类型 | 合并风险 |") {
		t.Errorf("comment missing metadata table header: %s", comment)
	}
	if !strings.Contains(comment, "4 / 5") {
		t.Errorf("comment missing confidence score: %s", comment)
	}
	if !strings.Contains(comment, "Security Fix") {
		t.Errorf("comment missing category: %s", comment)
	}
	if !strings.Contains(comment, "Low") {
		t.Errorf("comment missing merge risk: %s", comment)
	}

	// 检查测试审计部分
	if !strings.Contains(comment, "### 🧪 测试覆盖与回归审计") {
		t.Errorf("comment missing missing-tests section: %s", comment)
	}
	if !strings.Contains(comment, "缺少针对嵌套非法 prototype 属性时的拦截测试") {
		t.Errorf("comment missing specific test warning: %s", comment)
	}

	// 检查行动项与建议代码块
	if !strings.Contains(comment, "### 🛠️ 建议采纳与重构示范") {
		t.Errorf("comment missing suggestions section: %s", comment)
	}
	if !strings.Contains(comment, "建议改用字段白名单以防绕过") {
		t.Errorf("comment missing suggestion title: %s", comment)
	}
	if !strings.Contains(comment, "```javascript") {
		t.Errorf("comment missing suggestion code block: %s", comment)
	}
}

func TestMaintainerVerdictAndSensitiveAssets(t *testing.T) {
	diff := `diff --git a/.github/workflows/ci.yml b/.github/workflows/ci.yml
--- a/.github/workflows/ci.yml
+++ b/.github/workflows/ci.yml
@@ -1 +1 @@
-old
+new
diff --git a/package-lock.json b/package-lock.json
--- a/package-lock.json
+++ b/package-lock.json
@@ -1 +1 @@
-old
+new`

	rep := scanDiffHeuristics(diff)
	if len(rep.sensitiveAssets) != 2 {
		t.Fatalf("expected 2 sensitive assets, got %d: %v", len(rep.sensitiveAssets), rep.sensitiveAssets)
	}

	res := &CodeReviewResult{
		Summary: "更新 CI 与依赖",
		Score:   90,
	}
	applyScoreAndRiskGuards(res, rep)

	if res.MaintainerVerdict != "Needs Manual Review" {
		t.Errorf("expected MaintainerVerdict Needs Manual Review, got %s", res.MaintainerVerdict)
	}
	if len(res.SensitiveAssets) != 2 {
		t.Errorf("expected 2 sensitive assets in res, got %d", len(res.SensitiveAssets))
	}

	comment := FormatPRComment(res)
	if !strings.Contains(comment, "维护者裁决") {
		t.Errorf("expected comment to contain 维护者裁决, got: %s", comment)
	}
	if !strings.Contains(comment, "Needs Manual Review") {
		t.Errorf("expected comment to contain Needs Manual Review, got: %s", comment)
	}
	if !strings.Contains(comment, "哨兵关键资产变动预警") {
		t.Errorf("expected comment to contain 哨兵关键资产变动预警, got: %s", comment)
	}
}

func TestSensitiveAssetDetectionAndMaintainerVerdictLadder(t *testing.T) {
	// 1. CI/CD 工作流文件变更嗅探
	diffCI := `diff --git a/.github/workflows/deploy.yml b/.github/workflows/deploy.yml
--- a/.github/workflows/deploy.yml
+++ b/.github/workflows/deploy.yml
@@ -1 +1 @@
+name: deploy`
	repCI := scanDiffHeuristics(diffCI)
	if len(repCI.sensitiveAssets) == 0 {
		t.Fatal("expected CI/CD workflow to be detected as sensitive asset")
	}

	// 2. 依赖锁定清单变更嗅探
	diffLock := `diff --git a/package-lock.json b/package-lock.json
--- a/package-lock.json
+++ b/package-lock.json
@@ -1 +1 @@
+{"name": "foo"}`
	repLock := scanDiffHeuristics(diffLock)
	if len(repLock.sensitiveAssets) == 0 {
		t.Fatal("expected package-lock.json to be detected as sensitive asset")
	}

	// 3. 裁决阶梯测试：当存在敏感资产时，即使模型返回 Ready to Merge，也必须强制修正为 Needs Manual Review
	resSafeLooking := &CodeReviewResult{
		Score:             95,
		MaintainerVerdict: "Ready to Merge",
	}
	applyScoreAndRiskGuards(resSafeLooking, repCI)
	if resSafeLooking.MaintainerVerdict != "Needs Manual Review" {
		t.Fatalf("expected Needs Manual Review when sensitive assets modified, got %s", resSafeLooking.MaintainerVerdict)
	}

	// 4. 裁决阶梯测试：当出现安全漏洞时，即使存在敏感资产，也必须升至最高阶梯 Block Risk
	resVulnerable := &CodeReviewResult{
		Score:             40,
		SecurityRisks:     []string{"恶意反弹 shell 脚本"},
		MaintainerVerdict: "Needs Manual Review",
	}
	applyScoreAndRiskGuards(resVulnerable, repCI)
	if resVulnerable.MaintainerVerdict != "Block Risk" {
		t.Fatalf("expected Block Risk when security risk present, got %s", resVulnerable.MaintainerVerdict)
	}

	// 5. 格式化输出中验证徽章与敏感资产展示
	comment := FormatPRComment(resSafeLooking)
	if !strings.Contains(comment, "🟠 Needs Manual Review") {
		t.Fatalf("expected comment to contain Needs Manual Review badge, got: %s", comment)
	}
	if !strings.Contains(comment, "🚨 哨兵关键资产变动预警") {
		t.Fatalf("expected comment to contain sensitive asset section, got: %s", comment)
	}
}

// TestDeriveMaintainerVerdictLadder 守护裁决阶梯的唯一实现：
// FormatPRComment 的兜底分支与 applyScoreAndRiskGuards 必须共用 deriveMaintainerVerdict，
// 否则缺裁决字段的存量报告会与写库时的结论分叉（此前兜底分支漏判 Critical 合并风险）。
func TestDeriveMaintainerVerdictLadder(t *testing.T) {
	base := func() *CodeReviewResult { return &CodeReviewResult{Score: 90} }

	if got := deriveMaintainerVerdict(base()); got != "Ready to Merge" {
		t.Errorf("干净高分报告应判 Ready to Merge，got %s", got)
	}

	tests := []struct {
		name string
		mod  func(*CodeReviewResult)
		want string
	}{
		{"缺测试", func(r *CodeReviewResult) { r.MissingTests = []string{"缺用例"} }, "Needs Tests"},
		{"低分", func(r *CodeReviewResult) { r.Score = 79 }, "Needs Tests"},
		{"敏感资产", func(r *CodeReviewResult) { r.SensitiveAssets = []string{"⚙️ ci.yml"} }, "Needs Manual Review"},
		{"Critical 合并风险", func(r *CodeReviewResult) { r.MergeRisk = "Critical" }, "Block Risk"},
		{"安全风险", func(r *CodeReviewResult) { r.SecurityRisks = []string{"后门"} }, "Block Risk"},
		{"极低分", func(r *CodeReviewResult) { r.Score = 59 }, "Block Risk"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			res := base()
			tc.mod(res)
			if got := deriveMaintainerVerdict(res); got != tc.want {
				t.Errorf("deriveMaintainerVerdict = %s, want %s", got, tc.want)
			}
		})
	}

	// 敏感资产不得越级覆盖更高的安全阶梯
	escalated := &CodeReviewResult{Score: 95, SensitiveAssets: []string{"⚙️ ci.yml"}, MergeRisk: "Critical"}
	if got := deriveMaintainerVerdict(escalated); got != "Block Risk" {
		t.Errorf("敏感资产叠加 Critical 应判 Block Risk，got %s", got)
	}

	// 缺裁决字段的存量报告：评论兜底必须与写库口径一致
	legacy := &CodeReviewResult{Score: 88, MergeRisk: "Critical"}
	if got := FormatPRComment(legacy); !strings.Contains(got, "🔴 Block Risk") {
		t.Errorf("存量报告兜底应渲染 Block Risk 徽章，got: %s", got)
	}
}

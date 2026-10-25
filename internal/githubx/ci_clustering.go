package githubx

import (
	"strings"
)

// FailureCategory CI 故障分类。
type FailureCategory string

const (
	CategoryFlakyNetwork FailureCategory = "flaky_network" // 网络超时/依赖拉取中断
	CategoryFlakyRunner  FailureCategory = "flaky_runner"  // 运行器挂掉/资源超限/OOM
	CategoryTestFailure  FailureCategory = "test_failure"  // 单元测试/集成测试断言失败
	CategoryBuildFailure FailureCategory = "build_failure" // 编译错误/语法错误/类型错误
	CategoryLintFailure  FailureCategory = "lint_failure"  // 静态代码检查/格式化不合规
	CategoryTimeout      FailureCategory = "timed_out"     // 任务整体超时
	CategoryUnknown      FailureCategory = "unknown"
)

// FailureAttribution 故障归因分析结论。
type FailureAttribution struct {
	Category   FailureCategory `json:"category"`
	Label      string          `json:"label"`      // 中文友好标签
	CanRetry   bool            `json:"can_retry"`  // 是否适合一键重试
	Confidence float64         `json:"confidence"` // 置信度 0.0 ~ 1.0
	Snippet    string          `json:"snippet,omitempty"`
	Advice     string          `json:"advice"`
}

// ClassifyCIFailure 基于运行结论与错误日志/上下文进行确定性故障聚类。
func ClassifyCIFailure(conclusion string, detail string) FailureAttribution {
	if conclusion == "timed_out" {
		return FailureAttribution{
			Category:   CategoryTimeout,
			Label:      "运行超时",
			CanRetry:   true,
			Confidence: 0.95,
			Advice:     "任务执行达到超时上限，偶发超时建议一键重试，持续超时需检查是否存在死循环",
		}
	}

	lower := strings.ToLower(detail)

	// 1. 偶发网络/代理故障
	networkPatterns := []string{
		"dial tcp timeout",
		"connect: connection refused",
		"i/o timeout",
		"econnreset",
		"could not resolve host",
		"connection reset by peer",
		"tls handshake timeout",
		"failed to fetch",
		"http 502",
		"http 503",
		"http 504",
		"gateway timeout",
		"error downloading",
		"failed to download repository",
	}
	for _, p := range networkPatterns {
		if strings.Contains(lower, p) {
			return FailureAttribution{
				Category:   CategoryFlakyNetwork,
				Label:      "网络/源偶发抖动 (Flaky)",
				CanRetry:   true,
				Confidence: 0.90,
				Snippet:    extractSnippet(detail, p),
				Advice:     "上游网络或包源偶发抖动，与业务代码无关，建议一键重跑工作流 (Rerun)",
			}
		}
	}

	// 2. 偶发 Runner 异常 / OOM
	runnerPatterns := []string{
		"runner lost communication",
		"the operation was canceled",
		"out of memory",
		"killed",
		"exit code 137",
		"no space left on device",
	}
	for _, p := range runnerPatterns {
		if strings.Contains(lower, p) {
			return FailureAttribution{
				Category:   CategoryFlakyRunner,
				Label:      "运行环境/资源异常 (Flaky)",
				CanRetry:   true,
				Confidence: 0.85,
				Snippet:    extractSnippet(detail, p),
				Advice:     "GitHub Actions 运行器被强杀或资源不足，建议重试或清理缓存",
			}
		}
	}

	// 3. 测试断言失败
	testPatterns := []string{
		"fail:",
		"failed (failures=",
		"npm test failed",
		"cargo test failed",
		"pytest: error",
		"--- fail:",
		"tests failed",
		"assertionerror",
	}
	for _, p := range testPatterns {
		if strings.Contains(lower, p) {
			return FailureAttribution{
				Category:   CategoryTestFailure,
				Label:      "测试断言失败",
				CanRetry:   false,
				Confidence: 0.92,
				Snippet:    extractSnippet(detail, p),
				Advice:     "单元测试或集成测试未通过，请检查提交的代码逻辑，修复后再提交",
			}
		}
	}

	// 4. 静态检查 / 代码格式不合规
	lintPatterns := []string{
		"golangci-lint",
		"eslint",
		"prettier",
		"rubocop",
		"flake8",
		"black --check",
	}
	for _, p := range lintPatterns {
		if strings.Contains(lower, p) {
			return FailureAttribution{
				Category:   CategoryLintFailure,
				Label:      "静态检查/格式不符",
				CanRetry:   false,
				Confidence: 0.88,
				Snippet:    extractSnippet(detail, p),
				Advice:     "代码风格或静态检查未通过，请在本地运行 linter 修复后提交",
			}
		}
	}

	// 5. 编译 / 语法错误
	buildPatterns := []string{
		"syntax error",
		"cannot find package",
		"compilation error",
		"error ts",
		"undefined:",
		"build error",
	}
	for _, p := range buildPatterns {
		if strings.Contains(lower, p) {
			return FailureAttribution{
				Category:   CategoryBuildFailure,
				Label:      "编译/构建失败",
				CanRetry:   false,
				Confidence: 0.85,
				Snippet:    extractSnippet(detail, p),
				Advice:     "代码存在语法或编译类型错误，请在本地编译通过后提交",
			}
		}
	}

	return FailureAttribution{
		Category:   CategoryUnknown,
		Label:      "未分类执行失败",
		CanRetry:   true,
		Confidence: 0.50,
		Advice:     "请查看 GitHub Actions 完整运行日志定位具体原因",
	}
}

func extractSnippet(text, match string) string {
	lines := strings.Split(text, "\n")
	idx := -1
	lowerMatch := strings.ToLower(match)
	for i, l := range lines {
		if strings.Contains(strings.ToLower(l), lowerMatch) {
			idx = i
			break
		}
	}
	if idx == -1 {
		return ""
	}
	start := max(0, idx-1)
	end := min(len(lines), idx+3)
	return strings.TrimSpace(strings.Join(lines[start:end], "\n"))
}

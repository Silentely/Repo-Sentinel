package rules

import (
	"strings"
	"testing"
)

func TestDetectIssueRecurrence(t *testing.T) {
	curr := "[Bug] 执行任务时收到某些回复后停止并误标为完成"
	history := []string{
		"[Bug] 执行任务时收到回复后停止",
		"[Bug] 任务误标为完成",
		"feat: 优化首页加载性能",
	}

	report := DetectIssueRecurrence(curr, history)
	if !report.Recurrent {
		t.Fatalf("匹配到 2 次以上应判定为复发缺陷，got: %+v", report)
	}
	if !strings.Contains(report.Hint, "反复复发") {
		t.Errorf("提示文案应说明复发，got: %s", report.Hint)
	}
}

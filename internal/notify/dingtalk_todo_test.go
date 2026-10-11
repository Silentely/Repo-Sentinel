package notify

import (
	"strings"
	"testing"
)

func TestNormalizeDingTalkTodoList(t *testing.T) {
	input := `### 任务清单
- [x] 修复内存泄漏
- [ ] 补充集成测试
* [X] 文档更新`

	res := NormalizeDingTalkTodoList(input)

	if res.TotalTasks != 3 {
		t.Fatalf("expected 3 total tasks, got %d", res.TotalTasks)
	}
	if res.CompletedTasks != 2 {
		t.Errorf("expected 2 completed tasks, got %d", res.CompletedTasks)
	}
	// 2/3 ≈ 66.7%
	if res.Percent < 66.0 || res.Percent > 67.0 {
		t.Errorf("expected ~66.7 percent, got %f", res.Percent)
	}

	if !strings.Contains(res.FormattedText, "☑️ 修复内存泄漏") {
		t.Errorf("expected checked emoji, got:\n%s", res.FormattedText)
	}
	if !strings.Contains(res.FormattedText, "⬜ 补充集成测试") {
		t.Errorf("expected unchecked emoji, got:\n%s", res.FormattedText)
	}
}

func TestNormalizeDingTalkTodoList_Empty(t *testing.T) {
	res := NormalizeDingTalkTodoList("")
	if res.TotalTasks != 0 {
		t.Errorf("expected 0 tasks for empty input")
	}
}

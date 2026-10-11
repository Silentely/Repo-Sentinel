package notify

import (
	"fmt"
	"math"
	"regexp"
)

// TaskProgressResult 任务进度与转换后的文本
type TaskProgressResult struct {
	TotalTasks     int     `json:"total_tasks"`
	CompletedTasks int     `json:"completed_tasks"`
	Percent        float64 `json:"percent"`
	FormattedText  string  `json:"formatted_text"`
}

var (
	checkedPattern   = regexp.MustCompile(`(?m)^(\s*)[-*]\s+\[[xX]\]\s+(.*)$`)
	uncheckedPattern = regexp.MustCompile(`(?m)^(\s*)[-*]\s+\[\s*\]\s+(.*)$`)
)

// NormalizeDingTalkTodoList 将 GitHub GFM 复选框任务列表转换为钉钉友好排版
func NormalizeDingTalkTodoList(markdown string) TaskProgressResult {
	if markdown == "" {
		return TaskProgressResult{}
	}

	total := 0
	completed := 0

	// 统计并替换已勾选项
	replaced := checkedPattern.ReplaceAllStringFunc(markdown, func(m string) string {
		total++
		completed++
		sub := checkedPattern.FindStringSubmatch(m)
		indent := sub[1]
		content := sub[2]
		return fmt.Sprintf("%s☑️ %s", indent, content)
	})

	// 统计并替换未勾选项
	replaced = uncheckedPattern.ReplaceAllStringFunc(replaced, func(m string) string {
		total++
		sub := uncheckedPattern.FindStringSubmatch(m)
		indent := sub[1]
		content := sub[2]
		return fmt.Sprintf("%s⬜ %s", indent, content)
	})

	var percent float64
	if total > 0 {
		percent = math.Round((float64(completed)/float64(total))*1000) / 10
	}

	return TaskProgressResult{
		TotalTasks:     total,
		CompletedTasks: completed,
		Percent:        percent,
		FormattedText:  replaced,
	}
}

package rules

import (
	"fmt"
	"strings"
)

// IssueRecurrenceReport 缺陷复发评估结论。
type IssueRecurrenceReport struct {
	Recurrent bool   `json:"recurrent"`
	Count     int    `json:"count"`
	Hint      string `json:"hint"`
}

// DetectIssueRecurrence 评估当前问题是否在历史中多次反复发生。
func DetectIssueRecurrence(currentTitle string, historyTitles []string) IssueRecurrenceReport {
	cLower := strings.ToLower(strings.TrimSpace(currentTitle))
	if cLower == "" || len(historyTitles) == 0 {
		return IssueRecurrenceReport{}
	}

	matchCount := 0
	keywords := extractKeywords(cLower)

	for _, h := range historyTitles {
		hLower := strings.ToLower(strings.TrimSpace(h))
		if hLower == cLower {
			matchCount++
			continue
		}
		// 关键词重合度匹配
		hit := 0
		for _, kw := range keywords {
			if strings.Contains(hLower, kw) {
				hit++
			}
		}
		if len(keywords) > 0 && hit >= len(keywords)/2+1 {
			matchCount++
		}
	}

	if matchCount >= 2 {
		return IssueRecurrenceReport{
			Recurrent: true,
			Count:     matchCount,
			Hint:      fmt.Sprintf("🔄 历史出现过 %d 次同类缺陷，疑似反复复发", matchCount),
		}
	}

	return IssueRecurrenceReport{
		Recurrent: false,
		Count:     matchCount,
	}
}

func extractKeywords(s string) []string {
	words := strings.Fields(s)
	var res []string
	for _, w := range words {
		clean := strings.Trim(w, "[](){}:,#")
		if len(clean) >= 3 {
			res = append(res, clean)
		}
	}
	return res
}

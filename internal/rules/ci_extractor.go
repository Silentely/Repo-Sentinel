package rules

import (
	"regexp"
	"strings"
)

var errorPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)\b(fail(?:ed|ure)?:\s*.+)`),
	regexp.MustCompile(`(?i)\b(panic:\s*.+)`),
	regexp.MustCompile(`(?i)\b(error:\s*.+)`),
	regexp.MustCompile(`(?i)\b(fatal:\s*.+)`),
	regexp.MustCompile(`(?i)\b(exit status \d+)`),
	regexp.MustCompile(`(?i)\b(npm ERR!\s*.+)`),
	regexp.MustCompile(`(?i)\b(undefined:\s*.+)`),
	regexp.MustCompile(`(?i)\b(cannot find package\s*.+)`),
	regexp.MustCompile(`(?i)\b(assertionerror:\s*.+)`),
}

// ExtractHeuristicErrorLines 从失败步骤或日志行中提取最关键的错误特征（最多返回 3 条）。
func ExtractHeuristicErrorLines(lines []string) []string {
	var results []string
	seen := make(map[string]bool)

	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		for _, re := range errorPatterns {
			if loc := re.FindString(trimmed); loc != "" {
				loc = strings.TrimSpace(loc)
				if !seen[loc] {
					seen[loc] = true
					results = append(results, loc)
					if len(results) >= 3 {
						return results
					}
				}
				break
			}
		}
	}

	return results
}

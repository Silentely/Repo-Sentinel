package rules

import (
	"regexp"
	"strings"
)

// CommitLintResult 约定式提交检查结果
type CommitLintResult struct {
	Valid      bool   `json:"valid"`
	Type       string `json:"type"`
	Scope      string `json:"scope"`
	Subject    string `json:"subject"`
	IsBreaking bool   `json:"is_breaking"`
	Score      int    `json:"score"` // 0 ~ 100
	Advice     string `json:"advice,omitempty"`
}

var convCommitPattern = regexp.MustCompile(`^([a-zA-Z]+)(\([a-zA-Z0-9_\-\./]+\))?(!)?:\s+(.+)$`)

var standardTypes = map[string]bool{
	"feat":     true,
	"fix":      true,
	"docs":     true,
	"style":    true,
	"refactor": true,
	"perf":     true,
	"test":     true,
	"build":    true,
	"ci":       true,
	"chore":    true,
	"revert":   true,
}

// LintCommitMessage 校验 Commit Message 第一行是否符合约定式提交规范并打分
func LintCommitMessage(message string) CommitLintResult {
	lines := strings.Split(strings.TrimSpace(message), "\n")
	if len(lines) == 0 || lines[0] == "" {
		return CommitLintResult{
			Valid:  false,
			Score:  0,
			Advice: "提交信息为空",
		}
	}

	firstLine := strings.TrimSpace(lines[0])
	matches := convCommitPattern.FindStringSubmatch(firstLine)
	if len(matches) == 0 {
		return CommitLintResult{
			Valid:   false,
			Score:   30,
			Subject: firstLine,
			Advice:  "建议遵循格式 `<type>(<scope>): <subject>`，例如 `feat(auth): add oauth login`",
		}
	}

	commitType := strings.ToLower(matches[1])
	rawScope := matches[2]
	scope := ""
	if len(rawScope) > 2 {
		scope = rawScope[1 : len(rawScope)-1]
	}
	isBreaking := matches[3] == "!"
	subject := matches[4]

	score := 70
	if standardTypes[commitType] {
		score += 20
	}
	if scope != "" {
		score += 10
	}

	return CommitLintResult{
		Valid:      true,
		Type:       commitType,
		Scope:      scope,
		Subject:    subject,
		IsBreaking: isBreaking,
		Score:      score,
	}
}

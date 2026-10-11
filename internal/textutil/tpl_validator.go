package textutil

import (
	"strings"
)

// TemplateValidation 模板语法校验结论。
type TemplateValidation struct {
	Valid  bool     `json:"valid"`
	Issues []string `json:"issues,omitempty"`
}

// ValidateTemplateSyntax 静态检查通知模板中占位符的合法性与闭合配对。
func ValidateTemplateSyntax(tpl string) TemplateValidation {
	var issues []string

	openCount := strings.Count(tpl, "{{")
	closeCount := strings.Count(tpl, "}}")

	if openCount != closeCount {
		issues = append(issues, "占位符括号未完全闭合（{{ 与 }} 数量不一致）")
	}

	if strings.Contains(tpl, "{{{") || strings.Contains(tpl, "}}}") {
		issues = append(issues, "存在非法三重大括号嵌套")
	}

	return TemplateValidation{
		Valid:  len(issues) == 0,
		Issues: issues,
	}
}

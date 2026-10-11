package textutil

import (
	"testing"
)

func TestValidateTemplateSyntax(t *testing.T) {
	// 1. 合法模板
	okTpl := "项目: {{repo}}, 分支: {{branch}}"
	resOk := ValidateTemplateSyntax(okTpl)
	if !resOk.Valid || len(resOk.Issues) > 0 {
		t.Fatalf("合法模板判定失败: %+v", resOk)
	}

	// 2. 未闭合模板
	badTpl := "项目: {{repo, 分支: {{branch}}"
	resBad := ValidateTemplateSyntax(badTpl)
	if resBad.Valid || len(resBad.Issues) == 0 {
		t.Fatalf("未闭合模板应报错: %+v", resBad)
	}
}

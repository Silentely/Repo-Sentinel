package rules

import (
	"testing"
)

func TestLintCommitMessage(t *testing.T) {
	// 标准提交：带作用域，标准类型
	res1 := LintCommitMessage("feat(auth): add google oauth2 support")
	if !res1.Valid {
		t.Fatalf("expected valid commit, got false")
	}
	if res1.Type != "feat" || res1.Scope != "auth" || res1.Subject != "add google oauth2 support" {
		t.Errorf("unexpected parsing: %+v", res1)
	}
	if res1.Score != 100 {
		t.Errorf("expected score 100, got %d", res1.Score)
	}

	// 破坏性变更
	res2 := LintCommitMessage("fix(api)!: remove deprecated v1 endpoint")
	if !res2.Valid || !res2.IsBreaking {
		t.Errorf("expected valid breaking change: %+v", res2)
	}

	// 无规范提交
	res3 := LintCommitMessage("fixed a bug in database")
	if res3.Valid {
		t.Errorf("expected non-conventional commit to be invalid")
	}
	if res3.Score > 50 {
		t.Errorf("expected low score, got %d", res3.Score)
	}
}

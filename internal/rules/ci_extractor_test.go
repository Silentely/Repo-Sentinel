package rules

import (
	"strings"
	"testing"
)

func TestExtractHeuristicErrorLines(t *testing.T) {
	lines := []string{
		"Run tests with go test ./...",
		"=== RUN   TestLogin",
		"--- FAIL: TestLogin (0.01s)",
		"    auth_test.go:42: expected token but got nil",
		"exit status 1",
		"FAIL\tpkg/auth\t0.035s",
		"npm ERR! code ELIFECYCLE",
	}

	extracted := ExtractHeuristicErrorLines(lines)
	if len(extracted) == 0 {
		t.Fatal("expected at least 1 error line extracted")
	}

	foundFail := false
	for _, l := range extracted {
		if strings.Contains(l, "FAIL: TestLogin") {
			foundFail = true
		}
	}
	if !foundFail {
		t.Errorf("expected FAIL line to be captured, got %v", extracted)
	}

	// 验证最多返回 3 条
	if len(extracted) > 3 {
		t.Errorf("expected at most 3 error lines, got %d", len(extracted))
	}
}

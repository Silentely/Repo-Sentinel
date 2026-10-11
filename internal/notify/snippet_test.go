package notify

import (
	"strings"
	"testing"
)

func TestCompactCodeBlock(t *testing.T) {
	raw := "\n\n  func test() {}  \n\n"
	got := CompactCodeBlock(raw)
	if got != "  func test() {}  " {
		t.Fatalf("expected trimmed empty lines, got: %q", got)
	}

	if CompactCodeBlock("\n   \n") != "" {
		t.Fatalf("all blanks should return empty")
	}
}

func TestCollapseLongCodeSnippets(t *testing.T) {
	// 短代码块（<= 8 行）不折叠
	shortHTML := "<pre><code>line 1\nline 2\nline 3</code></pre>"
	if got := CollapseLongCodeSnippets(shortHTML, 8); got != shortHTML {
		t.Errorf("短代码块不应被折叠, got: %s", got)
	}

	// 长代码块（> 8 行）自动折叠
	var sb strings.Builder
	sb.WriteString("<pre><code>")
	for i := 1; i <= 15; i++ {
		sb.WriteString("code line\n")
	}
	sb.WriteString("</code></pre>")
	longHTML := sb.String()

	collapsed := CollapseLongCodeSnippets(longHTML, 8)
	if !strings.Contains(collapsed, "<blockquote expandable>") {
		t.Errorf("超长代码块应被自动包装进 <blockquote expandable>, actual: %s", collapsed)
	}
}

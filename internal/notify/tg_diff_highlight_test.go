package notify

import (
	"strings"
	"testing"
)

func TestFormatTelegramDiffSnippet(t *testing.T) {
	diff := `@@ -1,5 +1,6 @@
 func main() {
-    println("old <tag>")
+    println("new <tag>")
+    println("another line")
 }`

	result := FormatTelegramDiffSnippet(diff, 10)

	if !strings.HasPrefix(result, "<pre><code class=\"language-diff\">") {
		t.Errorf("expected pre code diff wrapper, got:\n%s", result)
	}

	// 确认 HTML 实体已安全转义
	if strings.Contains(result, "<tag>") {
		t.Errorf("expected <tag> to be escaped to &lt;tag&gt;, got:\n%s", result)
	}
	if !strings.Contains(result, "&lt;tag&gt;") {
		t.Errorf("expected &lt;tag&gt; in result, got:\n%s", result)
	}

	// 确认以 @@ / + / - 开头的行保留
	if !strings.Contains(result, "+    println") || !strings.Contains(result, "-    println") {
		t.Errorf("diff lines missing: %s", result)
	}
}

func TestFormatTelegramDiffSnippet_OmittedLines(t *testing.T) {
	diff := "+ line 1\n+ line 2\n+ line 3\n+ line 4\n+ line 5"
	result := FormatTelegramDiffSnippet(diff, 2)

	if !strings.Contains(result, "... (3 lines omitted)") {
		t.Errorf("expected omitted line notice, got:\n%s", result)
	}
}

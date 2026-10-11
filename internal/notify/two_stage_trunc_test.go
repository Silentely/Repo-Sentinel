package notify

import (
	"strings"
	"testing"
)

func TestTruncateWithPreservedFooter(t *testing.T) {
	mainText := strings.Repeat("长长长正文内容", 100)
	footer := "⏰ 时间：2026-10-11 10:55 UTC"

	res := TruncateWithPreservedFooter(mainText, footer, 50)
	if !strings.HasSuffix(res, footer) {
		t.Fatalf("两阶段截断必须坚决保全底部页脚，实际尾部: %s", res)
	}
	if !strings.Contains(res, "…") {
		t.Fatalf("超长中间正文应包含省略号标记: %s", res)
	}
}

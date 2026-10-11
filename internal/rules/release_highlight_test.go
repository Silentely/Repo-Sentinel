package rules

import (
	"testing"
)

func TestExtractReleaseHighlights(t *testing.T) {
	notes := `
## Changelog
- feat: 新增 Telegram 富文本排版支持
- fix: 修复网络超时导致的重复重试
- feat(api): 增加监控健康检查接口
- BREAKING: 移除旧版弃用环境变量
`

	hl := ExtractReleaseHighlights(notes)
	if len(hl.Features) < 2 {
		t.Fatalf("expected at least 2 features, got: %d", len(hl.Features))
	}
	if len(hl.Fixes) < 1 {
		t.Fatalf("expected at least 1 fix, got: %d", len(hl.Fixes))
	}
	if len(hl.Breaking) < 1 {
		t.Fatalf("expected at least 1 breaking change, got: %d", len(hl.Breaking))
	}
}

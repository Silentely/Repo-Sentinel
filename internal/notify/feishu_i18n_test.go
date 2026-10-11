package notify

import (
	"testing"
)

func TestBuildFeishuI18nHeader(t *testing.T) {
	header := BuildFeishuI18nHeader("每日摘要", "Daily Digest")
	if header["tag"] != "plain_text" {
		t.Errorf("expected plain_text tag, got %v", header["tag"])
	}
	i18n, ok := header["i18n"].(map[string]string)
	if !ok {
		t.Fatalf("expected i18n map, got %v", header["i18n"])
	}
	if i18n["zh_cn"] != "每日摘要" || i18n["en_us"] != "Daily Digest" {
		t.Errorf("i18n mapping mismatch: %+v", i18n)
	}
}

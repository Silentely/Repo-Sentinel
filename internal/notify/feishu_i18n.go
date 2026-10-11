package notify

import "strings"

// BuildFeishuI18nHeader 构建飞书卡片双语标题头。
func BuildFeishuI18nHeader(zhTitle, enTitle string) map[string]any {
	zh := strings.TrimSpace(zhTitle)
	en := strings.TrimSpace(enTitle)
	if en == "" {
		en = zh
	}
	return map[string]any{
		"tag":     "plain_text",
		"content": zh,
		"i18n": map[string]string{
			"zh_cn": zh,
			"en_us": en,
		},
	}
}

package notify

import (
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// BuildWeComTextCardPayload 构建企业微信原生 textcard 图文卡片请求体。
func BuildWeComTextCardPayload(title, description, rawURL string) map[string]any {
	btnText := store.GitHubViewLabel
	if strings.TrimSpace(btnText) == "" {
		btnText = "详情"
	}

	return map[string]any{
		"msgtype": "textcard",
		"textcard": map[string]any{
			"title":       title,
			"description": description,
			"url":         rawURL,
			"btntxt":      btnText,
		},
	}
}

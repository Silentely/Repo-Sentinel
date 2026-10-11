package notify

// BuildFeishuCollapsibleElement 构建飞书卡片原生的折叠展开容器组件。
func BuildFeishuCollapsibleElement(headerText, bodyMarkdown string) map[string]any {
	return map[string]any{
		"tag": "collapsible_panel",
		"header": map[string]any{
			"title": map[string]any{
				"tag":     "plain_text",
				"content": headerText,
			},
		},
		"elements": []any{
			map[string]any{
				"tag":     "markdown",
				"content": bodyMarkdown,
			},
		},
	}
}

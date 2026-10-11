package notify

// BuildFeishuPostFallbackPayload 将通知内容转译为飞书标准的 post 富文本回退载荷
func BuildFeishuPostFallbackPayload(title string, paragraphs []string) map[string]interface{} {
	var contentRows [][]map[string]string

	for _, p := range paragraphs {
		if p == "" {
			continue
		}
		row := []map[string]string{
			{
				"tag":  "text",
				"text": p,
			},
		}
		contentRows = append(contentRows, row)
	}

	return map[string]interface{}{
		"msg_type": "post",
		"content": map[string]interface{}{
			"post": map[string]interface{}{
				"zh_cn": map[string]interface{}{
					"title":   title,
					"content": contentRows,
				},
			},
		},
	}
}

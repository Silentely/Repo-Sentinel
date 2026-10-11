package notify

// BuildFeishuAtTag 构建飞书 @提及 人员元素
func BuildFeishuAtTag(userID, userName string) map[string]string {
	tag := map[string]string{
		"tag":     "at",
		"user_id": userID,
	}
	if userName != "" {
		tag["user_name"] = userName
	}
	return tag
}

// BuildFeishuAtAllTag 构建飞书 @所有人 元素
func BuildFeishuAtAllTag() map[string]string {
	return map[string]string{
		"tag":     "at",
		"user_id": "all",
	}
}

// BuildFeishuLinkTag 构建飞书可点击超链接元素
func BuildFeishuLinkTag(text, href string) map[string]string {
	return map[string]string{
		"tag":  "a",
		"text": text,
		"href": href,
	}
}

// BuildFeishuTextTag 构建飞书普通文本元素
func BuildFeishuTextTag(text string) map[string]string {
	return map[string]string{
		"tag":  "text",
		"text": text,
	}
}

// BuildFeishuParagraph 组装一行混合样式的富文本行
func BuildFeishuParagraph(elements ...map[string]string) []map[string]string {
	var row []map[string]string
	for _, el := range elements {
		if el != nil {
			row = append(row, el)
		}
	}
	return row
}

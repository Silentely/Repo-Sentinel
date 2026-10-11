package notify

// DingTalkBtn 描述钉钉 ActionCard 的按钮
type DingTalkBtn struct {
	Title     string `json:"title"`
	ActionURL string `json:"actionURL"`
}

// BuildDingTalkActionCardPayload 生成符合钉钉标准协议的独立跳转 ActionCard Payload
func BuildDingTalkActionCardPayload(title, markdownText string, btns []DingTalkBtn, horizontal bool) map[string]interface{} {
	btnOrientation := "0" // 0: 纵向排列
	if horizontal {
		btnOrientation = "1" // 1: 横向排列
	}

	btnList := make([]map[string]string, 0, len(btns))
	for _, b := range btns {
		if b.Title == "" || b.ActionURL == "" {
			continue
		}
		btnList = append(btnList, map[string]string{
			"title":     b.Title,
			"actionURL": b.ActionURL,
		})
	}

	return map[string]interface{}{
		"msgtype": "actionCard",
		"actionCard": map[string]interface{}{
			"title":          title,
			"text":           markdownText,
			"btnOrientation": btnOrientation,
			"btns":           btnList,
		},
	}
}

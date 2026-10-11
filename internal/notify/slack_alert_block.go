package notify

import (
	"fmt"
)

// BuildSlackEmergencyAlertBlocks 构建 Slack 紧急故障红色告警 Block 数组
func BuildSlackEmergencyAlertBlocks(title, details, incidentURL string, callChannel bool) []map[string]interface{} {
	var blocks []map[string]interface{}

	headerText := fmt.Sprintf(":rotating_light: *CRITICAL: %s*", title)
	if callChannel {
		headerText = "<!channel> " + headerText
	}

	// 1. 标题与呼叫块
	blocks = append(blocks, map[string]interface{}{
		"type": "section",
		"text": map[string]interface{}{
			"type": "mrkdwn",
			"text": headerText,
		},
	})

	// 2. 详情描述块
	if details != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "section",
			"text": map[string]interface{}{
				"type": "mrkdwn",
				"text": "> " + details,
			},
		})
	}

	// 3. 快速操作按钮块
	if incidentURL != "" {
		blocks = append(blocks, map[string]interface{}{
			"type": "actions",
			"elements": []map[string]interface{}{
				{
					"type": "button",
					"text": map[string]interface{}{
						"type": "plain_text",
						"text": "查看故障详情",
					},
					"style": "danger",
					"url":   incidentURL,
				},
			},
		})
	}

	return blocks
}

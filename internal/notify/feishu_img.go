package notify

// FeishuImageMode 定义飞书卡片图片的展示裁剪模式
type FeishuImageMode string

const (
	FeishuImageModeFitHorizontal FeishuImageMode = "fit_horizontal" // 宽度自适应
	FeishuImageModeCropCenter    FeishuImageMode = "crop_center"    // 居中裁剪
)

// BuildFeishuImageElement 构建飞书交互卡片标准的 img 元素对象
func BuildFeishuImageElement(imgKey, altText string, mode FeishuImageMode, preview bool) map[string]interface{} {
	if mode == "" {
		mode = FeishuImageModeFitHorizontal
	}
	if altText == "" {
		altText = "image"
	}

	elem := map[string]interface{}{
		"tag":       "img",
		"img_key":   imgKey,
		"alt": map[string]interface{}{
			"tag":     "plain_text",
			"content": altText,
		},
		"mode":    string(mode),
		"preview": preview,
	}

	return elem
}

// BuildFeishuImageFigure 构建带标题说明和图片的组合卡片元素列表
func BuildFeishuImageFigure(title, imgKey, altText, caption string) []map[string]interface{} {
	var elements []map[string]interface{}

	if title != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "div",
			"text": map[string]interface{}{
				"tag":     "lark_md",
				"content": "**" + title + "**",
			},
		})
	}

	elements = append(elements, BuildFeishuImageElement(imgKey, altText, FeishuImageModeFitHorizontal, true))

	if caption != "" {
		elements = append(elements, map[string]interface{}{
			"tag": "note",
			"elements": []map[string]interface{}{
				{
					"tag":     "plain_text",
					"content": caption,
				},
			},
		})
	}

	return elements
}

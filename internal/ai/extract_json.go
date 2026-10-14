package ai

import (
	"bytes"
	"encoding/json"
	"errors"
	"strings"
)

var (
	// ErrNoJSONFound 表示模型输出中未找到合法的 JSON 结构。
	ErrNoJSONFound = errors.New("ai: no valid JSON found in response")
)

// StripThinkingTags 剥离大模型输出中的思维链标签（<think>、<thought>、<reasoning>），复用 client 底层实现并去除首尾空白。
func StripThinkingTags(s string) string {
	return strings.TrimSpace(stripThinkingTags(s))
}

// CleanAndExtractJSON 从模型输出文本中剥离思考过程标签与外围自然语言，提取结构化的纯净 JSON。
func CleanAndExtractJSON(raw string) (string, error) {
	// 1. 去除 UTF-8 BOM
	cleaned := strings.TrimPrefix(raw, "\xef\xbb\xbf")
	// 2. 剥离思考过程标签与思考文本
	cleaned = StripThinkingTags(cleaned)
	if cleaned == "" {
		return "", ErrNoJSONFound
	}

	// 3. 策略 A：优先尝试从 Markdown 代码块提取，并在代码块内寻找合法 JSON
	if codeBlock := extractFromCodeFence(cleaned); codeBlock != "" {
		if valid := findFirstValidJSON(codeBlock); valid != "" {
			return valid, nil
		}
		if json.Valid([]byte(codeBlock)) {
			return codeBlock, nil
		}
	}

	// 4. 策略 B：在清理后的全文中寻找首个合法的 JSON 对象或数组
	if valid := findFirstValidJSON(cleaned); valid != "" {
		return valid, nil
	}

	// 5. 策略 C：若标准解析未通过（如末尾逗号等轻度缺陷），尝试根据最外层大括号或方括号做范围截取
	if bracket := extractOutermostBracket(cleaned); bracket != "" {
		return bracket, nil
	}

	// 6. 兜底：若去除两端空白后以大括号或中括号开头，返回清理文本由上层结构化解析器进一步处理
	if strings.HasPrefix(cleaned, "{") || strings.HasPrefix(cleaned, "[") {
		return cleaned, nil
	}
	return "", ErrNoJSONFound
}

// extractFromCodeFence 提取 Markdown 代码块中的正文。
func extractFromCodeFence(s string) string {
	startIdx := strings.Index(s, "```")
	if startIdx == -1 {
		return ""
	}
	rest := s[startIdx+3:]
	nlIdx := strings.IndexByte(rest, '\n')
	if nlIdx == -1 {
		return ""
	}
	contentStart := nlIdx + 1
	restContent := rest[contentStart:]
	endIdx := strings.LastIndex(restContent, "```")
	if endIdx == -1 {
		return strings.TrimSpace(restContent)
	}
	return strings.TrimSpace(restContent[:endIdx])
}

// findFirstValidJSON 使用标准 json.Decoder 在字符串中扫描首个完整的 JSON 对象或数组。
func findFirstValidJSON(s string) string {
	for i := 0; i < len(s); i++ {
		if s[i] == '{' || s[i] == '[' {
			dec := json.NewDecoder(strings.NewReader(s[i:]))
			var raw json.RawMessage
			if err := dec.Decode(&raw); err == nil && len(raw) > 0 {
				trimmed := bytes.TrimSpace(raw)
				if (trimmed[0] == '{' && trimmed[len(trimmed)-1] == '}') ||
					(trimmed[0] == '[' && trimmed[len(trimmed)-1] == ']') {
					return string(trimmed)
				}
			}
		}
	}
	return ""
}

// extractOutermostBracket 提取字符串中从首个左括号到末尾对应右括号的子串。
func extractOutermostBracket(s string) string {
	firstBrace := strings.IndexByte(s, '{')
	lastBrace := strings.LastIndexByte(s, '}')
	firstBracket := strings.IndexByte(s, '[')
	lastBracket := strings.LastIndexByte(s, ']')

	var candBrace string
	if firstBrace != -1 && lastBrace > firstBrace {
		candBrace = strings.TrimSpace(s[firstBrace : lastBrace+1])
	}

	var candBracket string
	if firstBracket != -1 && lastBracket > firstBracket {
		candBracket = strings.TrimSpace(s[firstBracket : lastBracket+1])
	}

	if candBrace != "" && candBracket != "" {
		if firstBrace < firstBracket {
			return candBrace
		}
		return candBracket
	}
	if candBrace != "" {
		return candBrace
	}
	return candBracket
}

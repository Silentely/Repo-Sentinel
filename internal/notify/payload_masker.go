package notify

import (
	"encoding/json"
	"strings"
)

// sensitiveKeyWords 是敏感字段的强特征子串：对键名做分隔符归一化后匹配，
// 使 privateKey / private_key / private-key 等写法命中同一规则。
var sensitiveKeyWords = []string{
	"token",
	"secret",
	"password",
	"passwd",
	"pwd",
	"credential",
	"privatekey",
	"authorization",
	"apikey",
	"accesskey",
	"clientsecret",
	"signature",
	"cookie",
	"bearer",
	"passphrase",
}

// MaskSensitiveJSON 解析 JSON 结构并递归掩码敏感凭据字段
func MaskSensitiveJSON(jsonBytes []byte) ([]byte, error) {
	var data interface{}
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return nil, err
	}

	sanitized := maskValue(data)
	return json.Marshal(sanitized)
}

func maskValue(val interface{}) interface{} {
	switch v := val.(type) {
	case map[string]interface{}:
		cleanMap := make(map[string]interface{})
		for k, child := range v {
			if isKeySensitive(k) {
				cleanMap[k] = "****** (REDACTED)"
			} else {
				cleanMap[k] = maskValue(child)
			}
		}
		return cleanMap

	case []interface{}:
		cleanList := make([]interface{}, len(v))
		for i, child := range v {
			cleanList[i] = maskValue(child)
		}
		return cleanList

	default:
		if s, ok := v.(string); ok && s != "" {
			// 键名不敏感但值内嵌凭据（如 "Authorization: Bearer xxx"、"token=abc"）时按内容形态兜底脱敏。
			return RedactSensitiveSecrets(s)
		}
		return v
	}
}

func isKeySensitive(key string) bool {
	normalized := normalizeKeyName(key)
	for _, kw := range sensitiveKeyWords {
		if strings.Contains(normalized, kw) {
			return true
		}
	}
	return false
}

// normalizeKeyName 去掉下划线、连字符与空格后小写化，统一不同命名风格后再做特征匹配。
func normalizeKeyName(key string) string {
	replacer := strings.NewReplacer("_", "", "-", "", " ", "")
	return replacer.Replace(strings.ToLower(key))
}

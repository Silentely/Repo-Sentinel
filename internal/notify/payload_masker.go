package notify

import (
	"encoding/json"
	"strings"
)

var sensitiveKeyWords = []string{
	"token",
	"secret",
	"password",
	"private_key",
	"authorization",
	"access_token",
	"refresh_token",
	"api_key",
	"apikey",
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
		return v
	}
}

func isKeySensitive(key string) bool {
	lower := strings.ToLower(key)
	for _, kw := range sensitiveKeyWords {
		if strings.Contains(lower, kw) {
			return true
		}
	}
	return false
}

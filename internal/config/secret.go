package config

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"

	"gopkg.in/yaml.v3"
)

const secretMask = "[REDACTED]"

// Secret 保存敏感文本；除 Reveal 外的展示与序列化出口始终返回掩码。
// 字段保持私有，避免调用方通过 string(secret) 绕过掩码边界。
type Secret struct {
	value string
}

// NewSecret 从受控入口创建 Secret。
func NewSecret(value string) Secret {
	return Secret{value: value}
}

// Reveal 仅供明确需要原始秘密的可信消费者调用。
func (s Secret) Reveal() string {
	return s.value
}

// UnmarshalYAML 仅接受标量节点，供配置文件受控载入明文。
func (s *Secret) UnmarshalYAML(node *yaml.Node) error {
	if node == nil || node.Kind == 0 || node.Tag == "!!null" {
		s.value = ""
		return nil
	}
	if node.Kind != yaml.ScalarNode {
		return fmt.Errorf("secret must be a scalar")
	}
	s.value = node.Value
	return nil
}

// UnmarshalText 提供受控文本解码，供环境变量等边界适配器使用。
func (s *Secret) UnmarshalText(value []byte) error {
	s.value = string(value)
	return nil
}

// UnmarshalJSON 提供受控 JSON 解码，同时避免暴露内部字段布局。
func (s *Secret) UnmarshalJSON(value []byte) error {
	var plain string
	if err := json.Unmarshal(value, &plain); err != nil {
		return fmt.Errorf("secret must be a string")
	}
	s.value = plain
	return nil
}

// String 返回固定掩码，避免普通字符串格式化泄漏。
func (Secret) String() string {
	return secretMask
}

// GoString 返回固定掩码，避免 %#v 格式化泄漏。
func (Secret) GoString() string {
	return secretMask
}

// Format 忽略格式动词并写入固定掩码，封住不匹配格式的诊断回显。
func (Secret) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, secretMask)
}

// MarshalText 返回掩码，避免文本编码器输出秘密。
func (Secret) MarshalText() ([]byte, error) {
	return []byte(secretMask), nil
}

// MarshalJSON 返回包含掩码的合法 JSON 字符串。
func (Secret) MarshalJSON() ([]byte, error) {
	return json.Marshal(secretMask)
}

// LogValue 返回掩码值，避免 slog 处理器输出秘密。
func (Secret) LogValue() slog.Value {
	return slog.StringValue(secretMask)
}

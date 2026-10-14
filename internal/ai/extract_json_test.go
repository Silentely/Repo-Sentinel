package ai

import (
	"encoding/json"
	"errors"
	"testing"
)

func TestCleanAndExtractJSON_VariousFormats(t *testing.T) {
	tests := []struct {
		name        string
		input       string
		expectErr   bool
		expectedKey string
		expectedVal string
	}{
		{
			name:        "纯净裸JSON",
			input:       `{"category":"Bug Report","summary":"panic on startup"}`,
			expectedKey: "category",
			expectedVal: "Bug Report",
		},
		{
			name: "带Markdown代码块的JSON",
			input: "```json\n" +
				`{"category":"Feature Request","summary":"add dark mode"}` +
				"\n```",
			expectedKey: "category",
			expectedVal: "Feature Request",
		},
		{
			name: "DeepSeek-R1思考标签加Markdown代码块",
			input: `<think>
经过对 Pull Request 变更内容的详细分析：
1. 涉及安全修复；
2. 单元测试完备。
综合评定风险等级为 Low。
</think>
这是为你生成的评估结果：
` + "```json\n" +
				`{"summary":"安全补丁与单元测试补充","score":95,"confidence":5}` +
				"\n```\n希望对你有所帮助！",
			expectedKey: "summary",
			expectedVal: "安全补丁与单元测试补充",
		},
		{
			name: "Claude风格thought标签加前后引导语",
			input: `<thought>
Issue intent: User reported installation failure on Alpine Linux.
Category should be Bug Report, priority P1 High.
</thought>
好的，针对该问题提供如下结构化分析：
{"category":"Bug Report","priority":"P1 High","summary":"Alpine Linux 下安装依赖报错"}
如有补充信息请随时提出。`,
			expectedKey: "priority",
			expectedVal: "P1 High",
		},
		{
			name: "思考标签未闭合场景（截断报错）",
			input: `<think>
正在思考当前上下文，尚未得出结论...`,
			expectErr: true,
		},
		{
			name: "字符串内嵌花括号与Markdown反引号",
			input: `<think>已分析</think>` + "\n" +
				"```json\n" +
				`{"summary":"重构提示","suggested_code":"func Foo() { return map[string]int{\"k\": 1} }"}` +
				"\n```",
			expectedKey: "summary",
			expectedVal: "重构提示",
		},
		{
			name: "后置说明文字中含有花括号干扰",
			input: `{"category":"Question","summary":"如何配置 Webhook"}
注意：请在设置页面将 {WEBHOOK_URL} 替换为你的真实公网地址。`,
			expectedKey: "category",
			expectedVal: "Question",
		},
		{
			name:        "前置说明代码块后跟JSON代码块",
			input:       "```bash\ncurl -s https://example.com\n```\n\n```json\n{\"category\":\"Bug Report\",\"summary\":\"网络超时\"}\n```",
			expectedKey: "category",
			expectedVal: "Bug Report",
		},
		{
			name:      "纯自然语言无JSON结构",
			input:     "抱歉，我无法解析此内容，因为 Diff 缺失。",
			expectErr: true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			got, err := CleanAndExtractJSON(tc.input)
			if tc.expectErr {
				if err == nil {
					t.Fatalf("期望返回错误，但实际成功返回: %s", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("提取失败: %v", err)
			}
			var parsed map[string]any
			if err := json.Unmarshal([]byte(got), &parsed); err != nil {
				t.Fatalf("提取的结果无法被合法反序列化: %v (内容: %s)", err, got)
			}
			if val, ok := parsed[tc.expectedKey]; !ok {
				t.Fatalf("未找到期望字段 %q，实际解析对象: %+v", tc.expectedKey, parsed)
			} else if s, ok := val.(string); ok && s != tc.expectedVal {
				t.Fatalf("期望字段 %s 值为 %q，实际为 %q", tc.expectedKey, tc.expectedVal, s)
			}
		})
	}
}

func TestExtractJSON_StripThinkingTagsWrapper(t *testing.T) {
	raw := "<think>\nThinking step 1\nThinking step 2\n</think>\nUseful output"
	stripped := StripThinkingTags(raw)
	if stripped != "Useful output" {
		t.Fatalf("期望输出 'Useful output'，实际: %q", stripped)
	}

	rawThought := "<thought>some thought</thought>Clean message"
	if StripThinkingTags(rawThought) != "Clean message" {
		t.Fatalf("期望输出 'Clean message'，实际: %q", StripThinkingTags(rawThought))
	}

	rawUnclosed := "<think>incomplete thought"
	if StripThinkingTags(rawUnclosed) != "" {
		t.Fatalf("未闭合思考标签应剥离为空，实际: %q", StripThinkingTags(rawUnclosed))
	}
}

func TestCleanAndExtractJSON_EmptyInput(t *testing.T) {
	_, err := CleanAndExtractJSON("")
	if !errors.Is(err, ErrNoJSONFound) {
		t.Fatalf("空输入期望返回 ErrNoJSONFound，实际: %v", err)
	}

	_, err = CleanAndExtractJSON("   \n\t  ")
	if !errors.Is(err, ErrNoJSONFound) {
		t.Fatalf("空白输入期望返回 ErrNoJSONFound，实际: %v", err)
	}
}

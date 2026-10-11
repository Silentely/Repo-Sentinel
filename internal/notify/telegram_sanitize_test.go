package notify

import (
	"strings"
	"testing"
)

func TestSanitizeTelegramHTML(t *testing.T) {
	// 1. 测试合法白名单标签保留
	validHTML := "<b>粗体</b> <i>斜体</i> <code>代码</code> <a href=\"https://github.com\">链接</a> <blockquote expandable>引用折叠</blockquote>"
	if got := SanitizeTelegramHTML(validHTML); got != validHTML {
		t.Errorf("合法标签不应被破坏，got: %s", got)
	}

	// 2. 测试 <br> 转换换行
	brHTML := "第一行<br>第二行<br/>第三行"
	expectedBR := "第一行\n第二行\n第三行"
	if got := SanitizeTelegramHTML(brHTML); got != expectedBR {
		t.Errorf("br 标签应转为换行，got: %s", got)
	}

	// 3. 测试非法标签剥离
	badHTML := "<div>段落内容</div> <span>文本</span> <script>alert('xss')</script>"
	expectedBad := "段落内容 文本 alert('xss')"
	if got := SanitizeTelegramHTML(badHTML); got != expectedBad {
		t.Errorf("非法标签应被剔除，got: %s", got)
	}

	// 4. 测试裸小于号转义
	lessHTML := "数量 < 10 且阈值 <= 5"
	if got := SanitizeTelegramHTML(lessHTML); !strings.Contains(got, "&lt; 10") || !strings.Contains(got, "&lt;= 5") {
		t.Errorf("裸小于号必须被转义，got: %s", got)
	}
}

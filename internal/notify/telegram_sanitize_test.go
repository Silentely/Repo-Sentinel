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

	// 5. 裸 & 必须转义：仓库名或标题含 R&D 时 Telegram 会报 400 解析失败
	if got := SanitizeTelegramHTML("R&D 团队维护"); got != "R&amp;D 团队维护" {
		t.Errorf("裸 & 应转义为 &amp;，got: %s", got)
	}
	// 已是实体形态的 & 保持原样，避免二次转义
	entity := "a &amp; b &lt;c&gt; &#39;d&#39;"
	if got := SanitizeTelegramHTML(entity); got != entity {
		t.Errorf("已有实体不应被二次转义，got: %s", got)
	}

	// 6. 连写尖括号：a<<b 两个 < 都不能留在标签位置
	if got := SanitizeTelegramHTML("a<<b"); got != "a&lt;&lt;b" {
		t.Errorf("连写裸小于号必须全部转义，got: %s", got)
	}

	// 7. code/pre 携带 class 属性属 Telegram 支持形态，必须原样保留
	langCode := "<pre><code class=\"language-go\">fmt.Println()</code></pre>"
	if got := SanitizeTelegramHTML(langCode); got != langCode {
		t.Errorf("带 class 的代码块不应被破坏，got: %s", got)
	}

	// 7b. 带连字符的白名单标签（tg-spoiler）必须保留，不能被当成裸小于号转义
	spoiler := "<tg-spoiler>剧透内容</tg-spoiler>"
	if got := SanitizeTelegramHTML(spoiler); got != spoiler {
		t.Errorf("tg-spoiler 标签应原样保留，got: %s", got)
	}

	// 7c. 未知命名实体会让 Telegram 解析失败，必须转义；官方支持的实体保持原样
	unknown := "a&notarealentity;b"
	if got := SanitizeTelegramHTML(unknown); got != "a&amp;notarealentity;b" {
		t.Errorf("未知命名实体应转义，got: %s", got)
	}

	// 8. 未闭合的标签前缀（<b 无 >）按裸小于号处理
	if got := SanitizeTelegramHTML("小于号 <b 未闭合"); !strings.Contains(got, "&lt;b") {
		t.Errorf("未闭合标签应转义为 &lt;b，got: %s", got)
	}
}

package notify

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSendTelegramIncludesReplyMarkup(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test message", "https://github.com/test/repo", "HTML")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	// 验证 reply_markup 存在
	rm, ok := receivedBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatal("期望 reply_markup 存在")
	}
	kb, ok := rm["inline_keyboard"].([]any)
	if !ok || len(kb) == 0 {
		t.Fatal("期望 inline_keyboard 非空")
	}
	// 验证按钮 URL
	row := kb[0].([]any)
	btn := row[0].(map[string]any)
	if btn["url"] != "https://github.com/test/repo" {
		t.Fatalf("期望按钮 URL 为 GitHub 链接，实际: %v", btn["url"])
	}
}

func TestSendTelegramWithoutURLNoReplyMarkup(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test message", "", "HTML")
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	if _, ok := receivedBody["reply_markup"]; ok {
		t.Fatal("无 htmlURL 时不应包含 reply_markup")
	}
}

func TestSendTelegramParses429RetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests","parameters":{"retry_after":120}}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test", "", "HTML")
	if err == nil {
		t.Fatal("期望 429 返回错误")
	}
	ra, ok := err.(*retryAfterError)
	if !ok {
		t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
	}
	if ra.seconds != 120 {
		t.Fatalf("期望 retry_after=120，实际: %d", ra.seconds)
	}
}

func TestSendTelegramParses429WithoutRetryAfter(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		w.Write([]byte(`{"ok":false,"error_code":429,"description":"Too Many Requests"}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test", "", "HTML")
	if err == nil {
		t.Fatal("期望 429 返回错误")
	}
	ra, ok := err.(*retryAfterError)
	if !ok {
		t.Fatalf("期望 retryAfterError，实际: %T %v", err, err)
	}
	if ra.seconds != 30 {
		t.Fatalf("期望默认 retry_after=30，实际: %d", ra.seconds)
	}
}

func TestSendTelegramNotConfigured(t *testing.T) {
	w := &Worker{Client: http.DefaultClient}
	err := w.sendTelegramDirect(t.Context(), "http://unused", "", "token", "text", "", "HTML")
	if err == nil {
		t.Fatal("期望空 chatID 返回错误")
	}
}

// 客户端禁跟随重定向（ErrUseLastResponse）：Telegram 端点返回 3xx 时消息未送达，
// 必须按可重试错误处理，此前穿透所有分支被标记 sent 静默丢失。
func TestSendTelegramRedirectIsFailure(t *testing.T) {
	for _, status := range []int{http.StatusMovedPermanently, http.StatusFound, http.StatusTemporaryRedirect} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
			}))
			t.Cleanup(srv.Close)
			w := &Worker{Client: srv.Client()}
			err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test", "", "HTML")
			if err == nil {
				t.Fatalf("%d 应返回错误（消息未送达）", status)
			}
			if want := fmt.Sprintf("telegram_redirect_%d", status); deliveryErrorCode(err) != want {
				t.Fatalf("期望错误码 %s，实际 %s", want, deliveryErrorCode(err))
			}
		})
	}
}

func TestTruncateTelegramTextKeepsShortText(t *testing.T) {
	short := strings.Repeat("a", telegramTextLimit-1)
	if got := truncateTelegramText(short); got != short {
		t.Fatalf("短文本不应截断，got len=%d", len(got))
	}
	// 恰好等于上限同样不截断。
	exact := strings.Repeat("a", telegramTextLimit)
	if got := truncateTelegramText(exact); got != exact {
		t.Fatal("等于上限的文本不应截断")
	}
}

func TestTruncateTelegramTextCutsLongText(t *testing.T) {
	long := strings.Repeat("a", telegramTextLimit+500)
	got := truncateTelegramText(long)
	if len([]rune(got)) > telegramTextLimit+20 {
		t.Fatalf("截断后长度应接近上限，got %d", len([]rune(got)))
	}
	if !strings.HasSuffix(got, "已截断）") {
		t.Fatalf("截断后应带省略提示，实际尾部: %q", got[len(got)-12:])
	}
}

func TestTruncateTelegramTextAvoidsBrokenTag(t *testing.T) {
	// 截断点落在 <a href="..." 开标签中间：必须回退到标签之前，不能把残缺标签发给 Telegram。
	text := strings.Repeat("a", telegramTextLimit-10) + `<a href="https://github.com/org/repo/issues/123"`
	got := truncateTelegramText(text)
	if strings.Contains(got, "<a href") {
		t.Fatalf("截断结果不应包含未闭合标签: %q", got[len(got)-60:])
	}
}

func TestTruncateTelegramTextAvoidsBrokenEntity(t *testing.T) {
	// 截断点落在实体中间（&amp 无分号）：必须剔除残缺实体。
	text := strings.Repeat("a", telegramTextLimit-3) + "&amp"
	got := truncateTelegramText(text)
	if strings.HasSuffix(got, "&amp") {
		t.Fatalf("截断结果不应以未闭合实体结尾: %q", got[len(got)-12:])
	}
}

func TestTruncateTelegramTextKeepsMultibyteIntact(t *testing.T) {
	// 多字节 UTF-8（中文）按码点截断，不得产生替换字符乱码。
	long := strings.Repeat("中", telegramTextLimit+100)
	got := truncateTelegramText(long)
	if strings.ContainsRune(got, '\uFFFD') {
		t.Fatal("截断结果不应包含替换字符乱码")
	}
}

func TestSendTelegramIssueTriageFormattingAndDedupLink(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	inputMsg := "<b>🟢 已打开｜#12 多个bug</b>\n────────────────\n📦 仓库：<code>org/repo</code>\n⏰ 时间：2026-10-07\n────────────────\n<a href=\"https://github.com/org/repo/issues/12\">🔗 在 GitHub 中查看</a>\n────────────────\n🤖 Issue 智能分析与回复建议\n🐛 类别：Bug Report ｜ 🔥 优先级：P1 High\n📝 核心诉求：修复登录问题\n💬 建议首响应回复：\n&gt; 你好，感谢反馈。\n&gt; 正在排查中。"

	err := w.sendTelegramDirect(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", inputMsg, "https://github.com/org/repo/issues/12", "HTML")
	if err != nil {
		t.Fatalf("发送失败: %v", err)
	}

	text, ok := receivedBody["text"].(string)
	if !ok {
		t.Fatalf("期望 text 字段存在")
	}

	// 1. 验证正文中冗余的 <a href="...">🔗 在 GitHub 中查看</a> 已被移除（避免与底部 inline button 重复割裂）
	if strings.Contains(text, "🔗 在 GitHub 中查看") {
		t.Errorf("正文不应包含多余的 GitHub 文本链接，got: %s", text)
	}

	// 2. 验证分析要素进入了 <blockquote expandable>
	if !strings.Contains(text, "<blockquote expandable>") || !strings.Contains(text, "🐛 类别：Bug Report") {
		t.Errorf("期望分析要素包含在可折叠引用块中，got: %s", text)
	}

	// 3. 验证建议首响应抽离为专属 <pre><code> 块，且已剥离 > 引用符
	if !strings.Contains(text, "💬 <b>建议首响应草稿（轻触文本直接复制）：</b>") {
		t.Errorf("期望包含建议首响应复制标题，got: %s", text)
	}
	if !strings.Contains(text, "<pre><code>你好，感谢反馈。\n正在排查中。</code></pre>") {
		t.Errorf("期望包含纯净 tap-to-copy code 块，got: %s", text)
	}
	if strings.Contains(text, "&gt;") || strings.Contains(text, "> 你好") {
		t.Errorf("建议草稿不应残留引用符号，got: %s", text)
	}

	// 4. 验证 inline_keyboard 保留了 GitHub 跳转按钮
	rm, ok := receivedBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatal("期望 reply_markup 存在")
	}
	kb := rm["inline_keyboard"].([]any)
	btn := kb[0].([]any)[0].(map[string]any)
	if btn["url"] != "https://github.com/org/repo/issues/12" {
		t.Errorf("期望按钮 URL 正确，got: %v", btn["url"])
	}
}

func TestFormatTelegramExpandableBlocks_GeneralAISection(t *testing.T) {
	input := "通知主体前缀\n────────────────\n🤖 告警分析\n影响：该漏洞可能导致远程代码执行。\n建议：尽快升级至最新版本。\n注意包含 Breaking Changes 变更。"
	output := formatTelegramExpandableBlocks(input)

	if !strings.Contains(output, "<blockquote expandable><b>🤖 告警分析</b>") {
		t.Fatalf("应包含可折叠块及标题，实际: %s", output)
	}
	if !strings.Contains(output, "<b>影响：</b>该漏洞可能导致远程代码执行。") {
		t.Fatalf("影响前缀应加粗，实际: %s", output)
	}
	if !strings.Contains(output, "<b>建议：</b>尽快升级至最新版本。") {
		t.Fatalf("建议前缀应加粗，实际: %s", output)
	}
	if !strings.Contains(output, "<b>Breaking Changes</b>") {
		t.Fatalf("Breaking Changes 应加粗，实际: %s", output)
	}
}

func TestBalanceTelegramHTML(t *testing.T) {
	// 1. 单个未闭合标签
	unclosed := "<blockquote><b>这是粗体内容"
	balanced := balanceTelegramHTML(unclosed)
	if balanced != "<blockquote><b>这是粗体内容</b></blockquote>" {
		t.Fatalf("应逆序闭合所有未闭合标签，实际: %s", balanced)
	}

	// 2. 已完全闭合的标签
	closed := "<b>完全闭合</b>"
	if balanceTelegramHTML(closed) != closed {
		t.Fatalf("已闭合标签不应改变，实际: %s", balanceTelegramHTML(closed))
	}
}

func TestTruncateTelegramTextBalancesOpenTags(t *testing.T) {
	longText := "<blockquote expandable><b>" + strings.Repeat("长长长长", 1200)
	truncated := truncateTelegramText(longText)

	if !strings.HasSuffix(truncated, "</b></blockquote>") {
		t.Fatalf("超长截断后末尾必须补齐闭合标签，避免 Telegram 报错 400，实际末尾: %s", truncated[len(truncated)-30:])
	}
}

func TestBuildTelegramInlineKeyboard_MultiRow(t *testing.T) {
	// 1. 无任何按钮时返回 nil
	if kb := buildTelegramInlineKeyboard("", ""); kb != nil {
		t.Fatalf("空参数应返回 nil, got %v", kb)
	}

	// 2. 主操作按钮（GitHub 链接 + 重试 token）
	kb := buildTelegramInlineKeyboard("https://github.com/test", "token123")
	if kb == nil {
		t.Fatal("期望返回 inline_keyboard")
	}
	rows := kb["inline_keyboard"].([][]map[string]string)
	if len(rows) != 1 || len(rows[0]) != 2 {
		t.Fatalf("期望 1 行 2 个按钮，got %v", rows)
	}

	// 3. 带有附加快捷操作行（如一键确认回执、静音）
	extraRow := []map[string]string{
		{"text": "✅ 确认已知", "callback_data": "ack:123"},
		{"text": "🔕 静音 1 小时", "callback_data": "mute:repo:1h"},
	}
	multiKb := buildTelegramInlineKeyboard("https://github.com/test", "", extraRow)
	multiRows := multiKb["inline_keyboard"].([][]map[string]string)
	if len(multiRows) != 2 {
		t.Fatalf("期望 2 行键盘，got %d 行", len(multiRows))
	}
	if multiRows[1][0]["callback_data"] != "ack:123" {
		t.Fatalf("第二行首个按钮 callback_data 不符: %v", multiRows[1][0])
	}
}

func TestSendTelegramWithExtraChatOpsActionRows(t *testing.T) {
	var receivedBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		if err := json.Unmarshal(body, &receivedBody); err != nil {
			t.Fatalf("无法解析请求体: %v", err)
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(srv.Close)

	w := &Worker{Client: srv.Client()}
	extraRows := [][]map[string]string{
		{{"text": "💬 采纳建议一键回复", "callback_data": "act:tok1"}},
		{{"text": "🏷️ 一键应用建议标签", "callback_data": "act:tok2"}},
	}
	err := w.sendTelegramDirectWithActions(t.Context(), srv.URL+"/sendMessage", "123", "fake-token", "test message", "https://github.com/test/repo", "HTML", "", extraRows...)
	if err != nil {
		t.Fatalf("不期望错误: %v", err)
	}

	rm, ok := receivedBody["reply_markup"].(map[string]any)
	if !ok {
		t.Fatal("期望 reply_markup 存在")
	}
	kb, ok := rm["inline_keyboard"].([]any)
	if !ok || len(kb) != 3 {
		t.Fatalf("期望 inline_keyboard 有 3 行 (1行URL + 2行动作)，实际: %d", len(kb))
	}
	// Row 1: URL
	row0 := kb[0].([]any)
	btn0 := row0[0].(map[string]any)
	if btn0["url"] != "https://github.com/test/repo" {
		t.Fatalf("期望第1行按钮为 GitHub 链接: %v", btn0)
	}
	// Row 2: Reply draft
	row1 := kb[1].([]any)
	btn1 := row1[0].(map[string]any)
	if btn1["text"] != "💬 采纳建议一键回复" || btn1["callback_data"] != "act:tok1" {
		t.Fatalf("期望第2行按钮为建议回复: %v", btn1)
	}
	// Row 3: Apply labels
	row2 := kb[2].([]any)
	btn2 := row2[0].(map[string]any)
	if btn2["text"] != "🏷️ 一键应用建议标签" || btn2["callback_data"] != "act:tok2" {
		t.Fatalf("期望第3行按钮为标签操作: %v", btn2)
	}
}

func TestExtractTelegramChatOpsActionRows(t *testing.T) {
	bodyJSON := map[string]any{
		"chatops_actions": []any{
			map[string]any{"text": "🚫 标记垃圾并关闭", "token": "tok_spam"},
			map[string]any{"text": "💬 采纳建议一键回复", "token": "tok_reply"},
		},
	}
	rows := extractTelegramChatOpsActionRows(bodyJSON)
	if len(rows) != 2 {
		t.Fatalf("期望提取 2 行按钮，实际: %d", len(rows))
	}
	if rows[0][0]["callback_data"] != "act:tok_spam" {
		t.Fatalf("期望 act:tok_spam，实际: %s", rows[0][0]["callback_data"])
	}
	if rows[1][0]["callback_data"] != "act:tok_reply" {
		t.Fatalf("期望 act:tok_reply，实际: %s", rows[1][0]["callback_data"])
	}
}

func TestExtractTelegramChatOpsActionRows_Pairing(t *testing.T) {
	bodyJSON := map[string]any{
		"chatops_actions": []any{
			map[string]any{"action": "issue_reply_draft", "text": "💬 采纳建议一键回复", "token": "tok_reply"},
			map[string]any{"action": "issue_request_details", "text": "📋 索取复现要素", "token": "tok_details"},
			map[string]any{"action": "issue_snooze", "text": "💤 延迟2小时提醒", "token": "tok_snooze"},
			map[string]any{"action": "issue_apply_labels", "text": "🏷️ 一键应用标签 (bug)", "token": "tok_labels"},
		},
	}
	rows := extractTelegramChatOpsActionRows(bodyJSON)
	if len(rows) != 3 {
		t.Fatalf("期望排版为 3 行（主操作独占、次要操作合并2列、标签独占），实际行数: %d", len(rows))
	}
	if len(rows[0]) != 1 || rows[0][0]["callback_data"] != "act:tok_reply" {
		t.Errorf("第一行应为主操作单按钮，实际: %v", rows[0])
	}
	if len(rows[1]) != 2 || rows[1][0]["callback_data"] != "act:tok_details" || rows[1][1]["callback_data"] != "act:tok_snooze" {
		t.Errorf("第二行应为索取要素与延迟提醒并排两列，实际: %v", rows[1])
	}
	if len(rows[2]) != 1 || rows[2][0]["callback_data"] != "act:tok_labels" {
		t.Errorf("第三行应为标签单按钮，实际: %v", rows[2])
	}
}

func TestExtractTelegramChatOpsActionRows_PRPairing(t *testing.T) {
	bodyJSON := map[string]any{
		"chatops_actions": []any{
			map[string]any{"action": "pr_quick_approve", "text": "✅ 快捷批准合入", "token": "tok_approve"},
			map[string]any{"action": "pr_snooze", "text": "💤 延迟2小时提醒", "token": "tok_snooze"},
		},
	}
	rows := extractTelegramChatOpsActionRows(bodyJSON)
	if len(rows) != 1 {
		t.Fatalf("期望 PR 快捷批准与待办延迟提醒并排为 1 行 2 列，实际行数: %d", len(rows))
	}
	if len(rows[0]) != 2 {
		t.Fatalf("期望第 1 行为 2 列按钮，实际: %d 列", len(rows[0]))
	}
	if rows[0][0]["callback_data"] != "act:tok_approve" || rows[0][1]["callback_data"] != "act:tok_snooze" {
		t.Errorf("按钮顺序或 callback_data 不符: %v", rows[0])
	}
}

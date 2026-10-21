package notify

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"html"
	"io"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// truncateRunes 安全截断字符串到指定 rune 数量，防止截断多字节字符。
func truncateRunes(s string, maxRunes int) string {
	if maxRunes <= 0 {
		return ""
	}
	if len(s) <= maxRunes || utf8.RuneCountInString(s) <= maxRunes {
		return s
	}
	count := 0
	for i := range s {
		if count == maxRunes {
			return strings.TrimRight(s[:i], " \n\r\t") + "…"
		}
		count++
	}
	return s
}

// themeSeverityKeywords 为判定主题色所用的**词级**关键词表。
// 必须用词边界匹配，禁止裸子串匹配：此前 "high" 会命中 "highlight(s)"、"success" 会命中
// "successful"/"succession"，把普通通知渲染成高危红色，削弱告警可信度。
var (
	themeCriticalKeywords = []string{
		"failure", "failed", "fail", "critical", "high", "block risk", "block-risk",
		"high risk", "high-risk", "high severity", "high-severity",
	}
	themeWarningKeywords = []string{
		"warning", "warn", "medium", "needs manual review", "needs tests", "needs test",
	}
	themeSuccessKeywords = []string{
		"success", "succeeded", "passed", "ready to merge", "fixed", "resolved",
	}

	themeCriticalChinese = []string{"严重", "高危", "失败", "阻断"}
	themeWarningChinese  = []string{"警告", "中危", "需人工审查", "需测试"}
	themeSuccessChinese  = []string{"成功", "通过", "可合并", "已修复"}
)

// containsAnyWord 判定文本（已小写）是否包含任一**完整单词**关键词。
// 词边界按非字母数字字符划分，故 "block risk" 这类含空格的短语仍可整体匹配。
func containsAnyWord(lowerText string, keywords []string) bool {
	for _, kw := range keywords {
		if containsWord(lowerText, kw) {
			return true
		}
	}
	return false
}

// containsWord 判定小写文本是否含完整单词/短语 kw（两侧不得紧邻字母数字字符）。
func containsWord(lowerText, kw string) bool {
	idx := 0
	for {
		pos := strings.Index(lowerText[idx:], kw)
		if pos < 0 {
			return false
		}
		start := idx + pos
		end := start + len(kw)
		if !isWordCharBefore(lowerText, start) && !isWordCharAt(lowerText, end) {
			return true
		}
		idx = start + 1
		if idx >= len(lowerText) {
			return false
		}
	}
}

func isWordCharBefore(s string, i int) bool {
	return i > 0 && isWordByte(s[i-1])
}

func isWordCharAt(s string, i int) bool {
	return i < len(s) && isWordByte(s[i])
}

// isWordByte 判定字节是否属于「单词字符」（ASCII 字母/数字/连字符）。
// 关键词表均为 ASCII，非 ASCII 字节（如中文）一律视为分隔符，使
// 「失败failure」这类中英混排也能正确切出边界。
// 连字符计入单词字符，使 "high-level"、"build-failed" 视为一个整体词，
// 避免 "high" 命中 "high-level" 这类良性复合词。
func isWordByte(b byte) bool {
	return (b >= 'a' && b <= 'z') || (b >= '0' && b <= '9') || b == '-'
}

// determineMessageTheme 根据标题与正文语义智能判定消息主题色：
// 飞书卡片 header template 支持: carmine (红/高危/失败), orange (橙/警示/需关注), turquoise (青绿/成功/健康), blue (默认蓝)。
// Discord embed color 支持: 0xEF4444, 0xF59E0B, 0x10B981, 0x3B82F6。
// 标题上的状态 emoji 为强信号直接命中；正文走词边界关键词匹配，避免子串误判。
func determineMessageTheme(title, body string) (feishuColor string, discordColor int) {
	if containsAnyRune(title, "❌", "🚨", "🔴") {
		return "carmine", 0xEF4444
	}
	if containsAnyRune(title, "⚠️", "⚠", "🟠", "🟡") {
		return "orange", 0xF59E0B
	}
	if containsAnyRune(title, "✅", "🟢", "🟣") {
		return "turquoise", 0x10B981
	}
	combined := title + " " + body
	lower := strings.ToLower(combined)
	if containsAnyWord(lower, themeCriticalKeywords) || containsAnySubstr(combined, themeCriticalChinese) {
		return "carmine", 0xEF4444
	}
	if containsAnyWord(lower, themeWarningKeywords) || containsAnySubstr(combined, themeWarningChinese) {
		return "orange", 0xF59E0B
	}
	if containsAnyWord(lower, themeSuccessKeywords) || containsAnySubstr(combined, themeSuccessChinese) {
		return "turquoise", 0x10B981
	}
	return "blue", 0x3B82F6
}

// Bark 严重度关键词与主题色关键词分开：主题色把 high 与 critical 一同渲染为红色，
// 而 Bark 的 critical 档会穿透 iOS 专注模式，只有确实需要立刻处理的内容才应命中。
var (
	barkCriticalKeywords = []string{"critical", "severe", "blocker", "outage", "failure", "failed", "fail"}
	barkHighKeywords     = []string{"high", "high severity", "high-severity", "warning", "warn"}
	barkCriticalChinese  = []string{"严重", "阻断", "失败", "宕机"}
	barkHighChinese      = []string{"高危", "警告", "中危", "需人工审查"}
)

// classifySeverity 按词边界把通知内容归入 Bark 分级的严重度档位，
// 避免 "high" 命中 "highlight" 这类子串误判；依赖更新类机器人推送归入低打扰。
func classifySeverity(title, body string) string {
	combined := title + " " + body
	lower := strings.ToLower(combined)
	switch {
	case containsAnyRune(title, "❌", "🚨", "🔴") || containsAnyWord(lower, barkCriticalKeywords) || containsAnySubstr(combined, barkCriticalChinese):
		return "critical"
	case containsAnyRune(title, "⚠️", "⚠", "🟠", "🟡") || containsAnyWord(lower, barkHighKeywords) || containsAnySubstr(combined, barkHighChinese):
		return "high"
	case containsAnyWord(lower, []string{"renovate", "dependabot"}) || containsAnySubstr(combined, []string{"依赖更新"}):
		return "low"
	default:
		return "medium"
	}
}

// containsAnySubstr 判定文本是否包含任一子串（如中文状态短语）。
func containsAnySubstr(text string, substrs []string) bool {
	for _, s := range substrs {
		if strings.Contains(text, s) {
			return true
		}
	}
	return false
}

// containsAnyRune 判定文本是否包含任一字面量标记（emoji/符号）。
func containsAnyRune(text string, marks ...string) bool {
	for _, m := range marks {
		if strings.Contains(text, m) {
			return true
		}
	}
	return false
}

// aiSectionDelimiter 为规则引擎追加 AI 分析段落时使用的分隔标记（分隔线 + 🤖 + 段标题）。
// 仅匹配 AI 段落：digest/摘要模式使用的不带 🤖 的分隔线不受影响。
const aiSectionDelimiter = "\n────────────────\n🤖 "

// splitAISections 将通知正文拆为「主体前缀 + AI 段落列表」，供各渠道按自身语法渲染。
// 无 AI 段落时原样返回输入（保留原始换行），段落为 [标题, 正文]（正文已去除首尾空白）。
// 收敛于此：此前企业微信/钉钉用两处 ReplaceAll、Telegram 用自带的 Split，三处各自维护分隔符字面量。
func splitAISections(text string) (prefix string, sections [][2]string) {
	parts := strings.Split(strings.ReplaceAll(text, "\r\n", "\n"), aiSectionDelimiter)
	if len(parts) == 1 {
		return text, nil
	}
	prefix = parts[0]
	for _, p := range parts[1:] {
		lines := strings.SplitN(strings.TrimSpace(p), "\n", 2)
		title := strings.TrimSpace(lines[0])
		var body string
		if len(lines) > 1 {
			body = strings.TrimSpace(lines[1])
		}
		sections = append(sections, [2]string{title, body})
	}
	return prefix, sections
}

// formatQuotedAISections 将 AI 段落渲染为引用块（企业微信/钉钉 markdown 语法）：
// 段标题与正文逐行加 "> " 前缀，使整段落在引用内，而非只把分隔线换成 "> 🤖" 让正文裸露在引用之外。
func formatQuotedAISections(text string) string {
	prefix, sections := splitAISections(text)
	if len(sections) == 0 {
		return text
	}
	var sb strings.Builder
	sb.WriteString(prefix)
	for _, sec := range sections {
		sb.WriteString("\n> 🤖 " + sec[0])
		if sec[1] == "" {
			continue
		}
		for _, line := range strings.Split(sec[1], "\n") {
			sb.WriteString("\n> " + line)
		}
	}
	return sb.String()
}

var (
	reHTMLStrong = regexp.MustCompile(`(?i)<(?:b|strong)>(.*?)</(?:b|strong)>`)
	reHTMLEm     = regexp.MustCompile(`(?i)<(?:i|em)>(.*?)</(?:i|em)>`)
	reHTMLEmTag  = regexp.MustCompile(`(?i)<em>([^<]*)</em>`)
	reHTMLITag   = regexp.MustCompile(`(?i)<i>([^<]*)</i>`)
	reHTMLCode   = regexp.MustCompile(`(?i)<code>(.*?)</code>`)
	reHTMLPre    = regexp.MustCompile(`(?is)<pre>(?:<code>)?(.*?)(?:</code>)?</pre>`)
	reHTMLBlock  = regexp.MustCompile(`(?is)<blockquote(?:[^>]*)>(.*?)</blockquote>`)
	reHTMLLinkMD = regexp.MustCompile(`(?i)<a\s+(?:[^>]*?\s+)?href=["']([^"']*)["'][^>]*>(.*?)</a>`)
	reHTMLTagsMD = regexp.MustCompile(`<[^>]+>`)
)

// htmlToMarkdown 将 HTML 转换为结构完备的 Markdown 格式，供企微、钉钉、飞书、Discord 等 Markdown 渠道渲染。
func htmlToMarkdown(s string) string {
	if !strings.Contains(s, "<") && !strings.Contains(s, "&") {
		return s
	}

	// 1. 代码块与引用块
	s = reHTMLPre.ReplaceAllString(s, "```\n$1\n```")
	s = reHTMLBlock.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHTMLBlock.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		lines := strings.Split(strings.TrimSpace(sub[1]), "\n")
		var quoted []string
		for _, line := range lines {
			trimmed := strings.TrimRight(line, "\r")
			if strings.TrimSpace(trimmed) == "" {
				quoted = append(quoted, ">")
			} else {
				quoted = append(quoted, "> "+trimmed)
			}
		}
		return "\n" + strings.Join(quoted, "\n") + "\n"
	})

	// 2. 行内样式
	s = reHTMLStrong.ReplaceAllString(s, "**$1**")
	s = reHTMLEm.ReplaceAllString(s, "*$1*")
	s = reHTMLCode.ReplaceAllString(s, "`$1`")
	s = reHTMLLinkMD.ReplaceAllString(s, "[$2]($1)")

	// 3. 剥离无用标签并反转义
	if strings.Contains(s, "<") {
		s = reHTMLTagsMD.ReplaceAllString(s, "")
	}
	return strings.TrimSpace(html.UnescapeString(s))
}

// sendFeishu 发送飞书/Lark 自定义机器人消息。
func (w *Worker) sendFeishu(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	now := time.Now().UTC()
	plainBody := truncateRunes(htmlToMarkdown(item.BodyText), 1500)

	elements := []any{
		map[string]any{
			"tag":     "markdown",
			"content": "**" + item.Title + "**\n\n" + plainBody,
		},
	}
	var actions []any
	if item.HTMLURL != "" {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": store.GitHubViewLabel},
			"url":  item.HTMLURL,
			"type": "primary",
		})
	}
	if token, ok := item.BodyJSON["chatops_token"].(string); ok && token != "" {
		actions = append(actions, map[string]any{
			"tag":  "button",
			"text": map[string]any{"tag": "plain_text", "content": "🔄 重试工作流"},
			"type": "danger",
			"value": map[string]any{
				"action":        "workflow_rerun",
				"chatops_token": token,
			},
		})
	}
	if raw, ok := item.BodyJSON["chatops_actions"]; ok && raw != nil {
		type actionItem struct {
			text   string
			token  string
			action string
		}
		var items []actionItem
		switch rawItems := raw.(type) {
		case []map[string]string:
			for _, m := range rawItems {
				if m["text"] != "" && m["token"] != "" {
					items = append(items, actionItem{text: m["text"], token: m["token"], action: m["action"]})
				}
			}
		case []any:
			for _, rawItem := range rawItems {
				if m, ok := rawItem.(map[string]any); ok {
					btnText, _ := m["text"].(string)
					token, _ := m["token"].(string)
					act, _ := m["action"].(string)
					if btnText != "" && token != "" {
						items = append(items, actionItem{text: btnText, token: token, action: act})
					}
				} else if m, ok := rawItem.(map[string]string); ok {
					if m["text"] != "" && m["token"] != "" {
						items = append(items, actionItem{text: m["text"], token: m["token"], action: m["action"]})
					}
				}
			}
		}
		for _, it := range items {
			btnType := "default"
			if it.action == "issue_mark_spam_and_close" {
				btnType = "danger"
			} else if it.action == "issue_reply_draft" || it.action == "pr_quick_approve" {
				btnType = "primary"
			}
			actions = append(actions, map[string]any{
				"tag":  "button",
				"text": map[string]any{"tag": "plain_text", "content": it.text},
				"type": btnType,
				"value": map[string]any{
					"action": it.action,
					"token":  it.token,
				},
			})
		}
	}
	elements = append(elements, map[string]any{
		"tag": "note",
		"elements": []any{
			map[string]any{"tag": "plain_text", "content": "🔔 RepoSentinel · " + now.Format("15:04 UTC")},
		},
	})
	if len(actions) > 0 {
		elements = append(elements, map[string]any{
			"tag":     "action",
			"actions": actions,
		})
	}

	feishuColor, _ := determineMessageTheme(item.Title, item.BodyText)
	payload := map[string]any{
		"msg_type": "interactive",
		"card": map[string]any{
			"header": map[string]any{
				"title":    map[string]any{"tag": "plain_text", "content": truncateRunes(item.Title, 100)},
				"template": feishuColor,
			},
			"elements": elements,
		},
	}

	// 飞书签名校验
	if secret != "" {
		ts := now.Unix()
		payload["timestamp"] = strconv.FormatInt(ts, 10)
		payload["sign"] = SignFeishu(ts, secret)
	}

	return w.postJSONChannel(ctx, ch, target, "feishu", payload)
}

// sendWeCom 发送企业微信群机器人消息。
func (w *Worker) sendWeCom(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToMarkdown(item.BodyText), 2000)
	plainBody = formatQuotedAISections(plainBody)
	themeColor, _ := determineMessageTheme(item.Title, item.BodyText)
	var formattedTitle string
	switch themeColor {
	case "carmine":
		formattedTitle = `<font color="warning">` + item.Title + `</font>`
	case "turquoise":
		formattedTitle = `<font color="info">` + item.Title + `</font>`
	default:
		formattedTitle = item.Title
	}

	var sb strings.Builder
	sb.WriteString("### " + formattedTitle + "\n\n")
	sb.WriteString(plainBody)
	if item.HTMLURL != "" {
		sb.WriteString("\n\n[" + store.GitHubViewLabel + "](" + item.HTMLURL + ")")
	}

	payload := map[string]any{
		"msgtype": "markdown",
		"markdown": map[string]any{
			"content": sb.String(),
		},
	}

	return w.postJSONChannel(ctx, ch, target, "wecom", payload)
}

// sendDingTalk 发送钉钉自定义机器人消息。
func (w *Worker) sendDingTalk(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	// 如果有加签密钥，追加 timestamp 与 sign query 参数
	reqURL := target
	if secret != "" {
		nowMs := time.Now().UTC().UnixMilli()
		// SignDingTalk 已对 base64 结果做 URL 转义，这里不可再转义一次。
		sign := SignDingTalk(nowMs, secret)

		sep := "?"
		if strings.Contains(reqURL, "?") {
			sep = "&"
		}
		reqURL = reqURL + sep + "timestamp=" + strconv.FormatInt(nowMs, 10) + "&sign=" + sign
	}

	plainBody := truncateRunes(htmlToMarkdown(item.BodyText), 2000)
	plainBody = formatQuotedAISections(plainBody)
	var payload map[string]any

	var dingBtns []map[string]string
	if item.HTMLURL != "" {
		dingBtns = append(dingBtns, map[string]string{"title": store.GitHubViewLabel, "actionURL": item.HTMLURL})
	}
	if opToken, ok := item.BodyJSON["chatops_token"].(string); ok && opToken != "" {
		dingBtns = append(dingBtns, map[string]string{"title": "🔄 重试工作流", "actionURL": item.HTMLURL})
	}

	if len(dingBtns) > 1 {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          item.Title,
				"text":           "### " + item.Title + "\n\n" + plainBody,
				"btnOrientation": "0",
				"btns":           dingBtns,
			},
		}
	} else if len(dingBtns) == 1 {
		payload = map[string]any{
			"msgtype": "actionCard",
			"actionCard": map[string]any{
				"title":          item.Title,
				"text":           "### " + item.Title + "\n\n" + plainBody,
				"singleTitle":    dingBtns[0]["title"],
				"singleURL":      dingBtns[0]["actionURL"],
				"btnOrientation": "0",
			},
		}
	} else {
		payload = map[string]any{
			"msgtype": "markdown",
			"markdown": map[string]any{
				"title": item.Title,
				"text":  "### " + item.Title + "\n\n" + plainBody,
			},
		}
	}

	return w.postJSONChannel(ctx, ch, reqURL, "dingtalk", payload)
}

// sendDiscord 发送 Discord Webhook 消息。
func (w *Worker) sendDiscord(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToMarkdown(item.BodyText), 2000)
	_, discordColor := determineMessageTheme(item.Title, item.BodyText)
	embed := map[string]any{
		"title":       item.Title,
		"description": plainBody,
		"color":       discordColor,
	}
	if item.HTMLURL != "" {
		embed["url"] = item.HTMLURL
	}

	payload := map[string]any{
		"embeds": []any{embed},
	}

	return w.postJSONChannel(ctx, ch, target, "discord", payload)
}

// sendBark 发送 Bark 推送消息。
func (w *Worker) sendBark(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	// 如果 target 未带 http/https 前缀，尝试补充默认 Bark 官方地址
	if !strings.HasPrefix(target, "http://") && !strings.HasPrefix(target, "https://") {
		target = "https://api.day.app/" + target
	}
	// 如果配置了 secret 且 target 结尾没有该 key
	if secret != "" && !strings.HasSuffix(target, secret) {
		target = strings.TrimRight(target, "/") + "/" + secret
	}
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	plainBody := truncateRunes(htmlToPlainText(item.BodyText), 1000)
	groupName := "RepoSentinel"

	if repo, ok := item.BodyJSON["repository"].(string); ok && repo != "" {
		groupName = repo
	} else if strings.Contains(item.Title, "：") {
		parts := strings.SplitN(item.Title, "：", 2)
		trimmedRepo := strings.TrimSpace(parts[0])
		for _, prefix := range []string{"📋 ", "🚨 ", "⚠️ ", "📌 ", "🔔 "} {
			trimmedRepo = strings.TrimPrefix(trimmedRepo, prefix)
		}
		trimmedRepo = strings.TrimPrefix(trimmedRepo, "🟢 ")
		trimmedRepo = strings.TrimPrefix(trimmedRepo, "⚫ ")
		trimmedRepo = strings.TrimPrefix(trimmedRepo, "🟣 ")
		if strings.Contains(trimmedRepo, "/") {
			groupName = trimmedRepo
		}
	}

	// 严重度按词边界判定：直接子串匹配会让 high 命中 highlight、success 命中 successful。
	sound, level := AssignBarkSoundAndLevel(classifySeverity(item.Title, item.BodyText))

	payload := map[string]any{
		"title": item.Title,
		"body":  plainBody,
		"group": groupName,
		"level": level,
	}
	if sound != "" {
		payload["sound"] = sound
	}
	if item.HTMLURL != "" {
		payload["url"] = item.HTMLURL
	}

	return w.postJSONChannel(ctx, ch, target, "bark", payload)
}

// htmlToSlackMrkdwn 将 HTML 转换为 Slack 专属 mrkdwn 语法（粗体 *text*、链接 <url|text>、斜体 _text_ 等）。
func htmlToSlackMrkdwn(s string) string {
	if !strings.Contains(s, "<") && !strings.Contains(s, "&") {
		return s
	}

	// 1. 块级标签
	s = reHTMLPre.ReplaceAllString(s, "```\n$1\n```")
	s = reHTMLBlock.ReplaceAllStringFunc(s, func(m string) string {
		sub := reHTMLBlock.FindStringSubmatch(m)
		if len(sub) < 2 {
			return m
		}
		lines := strings.Split(strings.TrimSpace(sub[1]), "\n")
		var quoted []string
		for _, line := range lines {
			trimmed := strings.TrimRight(line, "\r")
			if strings.TrimSpace(trimmed) == "" {
				quoted = append(quoted, ">")
			} else {
				quoted = append(quoted, "> "+trimmed)
			}
		}
		return "\n" + strings.Join(quoted, "\n") + "\n"
	})

	// 2. 行内样式：Slack 特有语法
	s = reHTMLStrong.ReplaceAllString(s, "*$1*")
	s = reHTMLEm.ReplaceAllString(s, "_${1}_")
	s = reHTMLEmTag.ReplaceAllString(s, "_${1}_")
	s = reHTMLITag.ReplaceAllString(s, "_${1}_")
	s = reHTMLCode.ReplaceAllString(s, "`$1`")
	// 3. 暂存 Slack 链接语法，避免后续 HTML 标签清理把其尖括号误删。
	var slackLinks []string
	s = reHTMLLinkMD.ReplaceAllStringFunc(s, func(raw string) string {
		parts := reHTMLLinkMD.FindStringSubmatch(raw)
		if len(parts) != 3 {
			return raw
		}
		slackLinks = append(slackLinks, "<"+parts[1]+"|"+parts[2]+">")
		return fmt.Sprintf("\x00SLACK_LINK_%d\x00", len(slackLinks)-1)
	})

	// 4. 剥离无用标签并反转义
	if strings.Contains(s, "<") {
		s = reHTMLTagsMD.ReplaceAllString(s, "")
	}
	for i, link := range slackLinks {
		s = strings.ReplaceAll(s, fmt.Sprintf("\x00SLACK_LINK_%d\x00", i), link)
	}
	return strings.TrimSpace(html.UnescapeString(s))
}

// sendSlack 发送 Slack Incoming Webhook 消息（Block Kit 格式）。
func (w *Worker) sendSlack(ctx context.Context, ch store.NotificationChannel, secret string, item store.NotificationOutbox) error {
	target := strings.TrimSpace(ch.Target)
	if err := validateWebhookURL(target, ch.AllowPrivate); err != nil {
		return err
	}

	headerText := truncateRunes(item.Title, 150)
	plainBody := truncateRunes(htmlToSlackMrkdwn(item.BodyText), 2800)

	blocks := []any{
		map[string]any{
			"type": "header",
			"text": map[string]any{
				"type":  "plain_text",
				"text":  headerText,
				"emoji": true,
			},
		},
		map[string]any{
			"type": "section",
			"text": map[string]any{
				"type": "mrkdwn",
				"text": plainBody,
			},
		},
	}

	var actionElements []any
	if item.HTMLURL != "" {
		actionElements = append(actionElements, map[string]any{
			"type": "button",
			"text": map[string]any{
				"type":  "plain_text",
				"text":  store.GitHubViewLabel,
				"emoji": true,
			},
			"url":       item.HTMLURL,
			"action_id": "view_on_github",
		})
	}
	if token, ok := item.BodyJSON["chatops_token"].(string); ok && token != "" {
		actionElements = append(actionElements, map[string]any{
			"type": "button",
			"text": map[string]any{
				"type":  "plain_text",
				"text":  "🔄 重试工作流",
				"emoji": true,
			},
			"value":     token,
			"action_id": "workflow_rerun",
			"style":     "danger",
		})
	}
	if raw, ok := item.BodyJSON["chatops_actions"]; ok && raw != nil {
		type actionItem struct {
			text   string
			token  string
			action string
		}
		var items []actionItem
		switch rawItems := raw.(type) {
		case []map[string]string:
			for _, m := range rawItems {
				if m["text"] != "" && m["token"] != "" {
					items = append(items, actionItem{text: m["text"], token: m["token"], action: m["action"]})
				}
			}
		case []any:
			for _, rawItem := range rawItems {
				if m, ok := rawItem.(map[string]any); ok {
					btnText, _ := m["text"].(string)
					token, _ := m["token"].(string)
					act, _ := m["action"].(string)
					if btnText != "" && token != "" {
						items = append(items, actionItem{text: btnText, token: token, action: act})
					}
				} else if m, ok := rawItem.(map[string]string); ok {
					if m["text"] != "" && m["token"] != "" {
						items = append(items, actionItem{text: m["text"], token: m["token"], action: m["action"]})
					}
				}
			}
		}
		for _, it := range items {
			btnObj := map[string]any{
				"type": "button",
				"text": map[string]any{
					"type":  "plain_text",
					"text":  it.text,
					"emoji": true,
				},
				"value":     it.token,
				"action_id": it.action,
			}
			if it.action == "issue_mark_spam_and_close" {
				btnObj["style"] = "danger"
			} else if it.action == "issue_reply_draft" || it.action == "pr_quick_approve" {
				btnObj["style"] = "primary"
			}
			actionElements = append(actionElements, btnObj)
		}
	}
	if len(actionElements) > 0 {
		blocks = append(blocks, map[string]any{
			"type":     "actions",
			"elements": actionElements,
		})
	}
	blocks = append(blocks, map[string]any{
		"type": "context",
		"elements": []any{
			map[string]any{
				"type": "mrkdwn",
				"text": "⏰ *RepoSentinel* · " + time.Now().UTC().Format("15:04 UTC"),
			},
		},
	})

	payload := map[string]any{
		"text":   truncateRunes(item.Title, 200),
		"blocks": blocks,
	}

	return w.postJSONChannel(ctx, ch, target, "slack", payload)
}

// postJSONChannel 通用 JSON 投递辅助方法。
func (w *Worker) postJSONChannel(ctx context.Context, ch store.NotificationChannel, targetURL, channelTag string, payload any) error {
	raw, err := json.Marshal(payload)
	if err != nil {
		return err
	}

	req, err := http.NewRequestWithContext(withAllowPrivate(ctx, ch.AllowPrivate), http.MethodPost, targetURL, bytes.NewReader(raw))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", webhookUserAgent())

	resp, err := w.Client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 300 && resp.StatusCode < 400 {
		return deliveryErrorf(channelTag+"_redirect_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode == 408 || resp.StatusCode == 425 || resp.StatusCode == 429 || resp.StatusCode >= 500 {
		if ra := parseRetryAfter(resp); ra > 0 {
			return &retryAfterError{seconds: ra, code: channelTag + "_retry_after"}
		}
		return deliveryErrorf(channelTag+"_status_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
	}
	if resp.StatusCode >= 400 {
		return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(resp.StatusCode), readBodyDetail(resp))
	}

	// 针对部分返回 200 但在 Body 中报告错误的平台（如飞书 errcode!=0 / 企业微信 errcode!=0 / 钉钉 errcode!=0）
	bodyBytes, _ := io.ReadAll(io.LimitReader(resp.Body, bodyDetailLimit))
	if len(bodyBytes) > 0 {
		if channelTag == "bark" {
			var barkResp struct {
				Code    int    `json:"code"`
				Message string `json:"message"`
			}
			if json.Unmarshal(bodyBytes, &barkResp) == nil && barkResp.Code != 0 && barkResp.Code != 200 {
				msg := barkResp.Message
				if msg == "" {
					msg = strings.TrimSpace(string(bodyBytes))
				}
				return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(barkResp.Code), msg)
			}
		} else {
			var statusResp struct {
				Code    int    `json:"code"`
				ErrCode int    `json:"errcode"`
				Msg     string `json:"msg"`
				ErrMsg  string `json:"errmsg"`
			}
			if json.Unmarshal(bodyBytes, &statusResp) == nil {
				if statusResp.Code != 0 {
					msg := statusResp.Msg
					if msg == "" {
						msg = statusResp.ErrMsg
					}
					if msg == "" {
						msg = strings.TrimSpace(string(bodyBytes))
					}
					return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(statusResp.Code), msg)
				}
				if statusResp.ErrCode != 0 {
					msg := statusResp.ErrMsg
					if msg == "" {
						msg = statusResp.Msg
					}
					if msg == "" {
						msg = strings.TrimSpace(string(bodyBytes))
					}
					return deliveryErrorf(channelTag+"_client_error_"+strconv.Itoa(statusResp.ErrCode), msg)
				}
			}
		}
	}

	return nil
}

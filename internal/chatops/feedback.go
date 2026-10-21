package chatops

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"

	"github.com/Silentely/Repo-Sentinel/internal/buildinfo"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TelegramFeedback 声明出站交互反馈端口。
type TelegramFeedback interface {
	AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool)
	AnswerActionCallback(ctx context.Context, callbackID, actionName string)
	EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, clickedData, actionName string, keyboard [][]map[string]any)
}

// DefaultTelegramFeedback 提供基于原生 Telegram Bot API 的出站反馈适配器。
type DefaultTelegramFeedback struct {
	Store      store.Store
	HTTPClient *http.Client
}

// NewDefaultTelegramFeedback 实例化反馈适配器。
func NewDefaultTelegramFeedback(st store.Store, client *http.Client) *DefaultTelegramFeedback {
	if client == nil {
		client = &http.Client{Timeout: 5 * time.Second}
	}
	return &DefaultTelegramFeedback{
		Store:      st,
		HTTPClient: client,
	}
}

func (f *DefaultTelegramFeedback) AnswerCallback(ctx context.Context, callbackID, text string, showAlert bool) {
	if f == nil || f.Store == nil || callbackID == "" {
		return
	}
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	botToken := ResolveTelegramBotToken(reqCtx, f.Store)
	if botToken == "" {
		return
	}

	payload := map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
		"show_alert":        showAlert,
	}
	raw, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)
	if strings.HasPrefix(botToken, "http://") || strings.HasPrefix(botToken, "https://") {
		url = botToken + "/answerCallbackQuery"
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent("ChatOpsFeedback"))
	resp, err := f.HTTPClient.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

func (f *DefaultTelegramFeedback) AnswerActionCallback(ctx context.Context, callbackID, actionName string) {
	if strings.HasPrefix(actionName, "done:") {
		f.AnswerCallback(ctx, callbackID, "ℹ️ 该操作已完成，无需重复操作", false)
		return
	}
	var text string
	switch actionName {
	case "issue_mark_duplicate":
		text = "🔗 已在 GitHub 关联历史议题并关闭"
	case "issue_reply_draft":
		text = "✅ 已成功在 GitHub 发布建议回复"
	case "issue_mark_spam_and_close":
		text = "🚫 已在 GitHub 标记垃圾内容并关闭"
	case "issue_apply_labels":
		text = "🏷️ 已成功在 GitHub 应用建议标签"
	case "issue_request_details":
		text = "📋 已在 GitHub 发布要素补充追问并打标"
	case "issue_snooze", "pr_snooze":
		text = "💤 已设置 2 小时后重新提醒"
	case "pr_quick_approve":
		text = "✅ 已成功批准 PR 合入"
	case "workflow_rerun":
		text = "🔄 已触发重新运行工作流"
	default:
		text = "✅ 操作已成功完成"
	}
	f.AnswerCallback(ctx, callbackID, text, false)
}

func (f *DefaultTelegramFeedback) EditMessageReplyMarkup(ctx context.Context, chatID int64, messageID int, clickedData, actionName string, keyboard [][]map[string]any) {
	if f == nil || f.Store == nil || chatID == 0 || messageID == 0 || len(keyboard) == 0 {
		return
	}
	reqCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
	defer cancel()

	botToken := ResolveTelegramBotToken(reqCtx, f.Store)
	if botToken == "" {
		return
	}

	newKeyboard := BuildUpdatedKeyboard(clickedData, actionName, keyboard)
	payload := map[string]any{
		"chat_id":    chatID,
		"message_id": messageID,
		"reply_markup": map[string]any{
			"inline_keyboard": newKeyboard,
		},
	}
	raw, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/editMessageReplyMarkup", botToken)
	if strings.HasPrefix(botToken, "http://") || strings.HasPrefix(botToken, "https://") {
		url = botToken + "/editMessageReplyMarkup"
	}
	req, err := http.NewRequestWithContext(reqCtx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", buildinfo.UserAgent("ChatOpsFeedback"))
	resp, err := f.HTTPClient.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

// BuildUpdatedKeyboard 依据动作类型及被点击的数据计算就地刷新后的键盘布局。
func BuildUpdatedKeyboard(clickedData, actionName string, keyboard [][]map[string]any) [][]map[string]any {
	var statusLabel string
	switch actionName {
	case "issue_mark_duplicate":
		statusLabel = "🔗 已关联关闭"
	case "issue_reply_draft":
		statusLabel = "✅ 已采纳回复"
	case "issue_mark_spam_and_close":
		statusLabel = "🚫 已标记关闭"
	case "issue_apply_labels":
		statusLabel = "🏷️ 已应用标签"
	case "issue_request_details":
		statusLabel = "📋 已索取要素"
	case "issue_snooze", "pr_snooze":
		statusLabel = "💤 已延迟提醒"
	case "pr_quick_approve":
		statusLabel = "✅ 已批准合入"
	case "workflow_rerun":
		statusLabel = "🔄 已触发重试"
	default:
		statusLabel = "✅ 已处理"
	}

	if actionName == "issue_mark_spam_and_close" || actionName == "issue_mark_duplicate" {
		// 标记垃圾或关联关闭后，收缩为单一完成态按钮，避免维护者误触
		return [][]map[string]any{
			{
				{"text": statusLabel, "callback_data": "done:" + actionName},
			},
		}
	}

	var newKeyboard [][]map[string]any
	for _, row := range keyboard {
		var newRow []map[string]any
		for _, btn := range row {
			btnCopy := make(map[string]any)
			for k, v := range btn {
				btnCopy[k] = v
			}
			if cd, ok := btnCopy["callback_data"].(string); ok && cd == clickedData {
				btnCopy["text"] = statusLabel
				btnCopy["callback_data"] = "done:" + actionName
			}
			newRow = append(newRow, btnCopy)
		}
		newKeyboard = append(newKeyboard, newRow)
	}
	return newKeyboard
}

// ResolveTelegramBotToken 按环境变量优先级与系统设置解析 Bot Token。
func ResolveTelegramBotToken(ctx context.Context, st store.Store) string {
	if envToken := strings.TrimSpace(os.Getenv("CHATOPS_TELEGRAM_BOT_TOKEN")); envToken != "" {
		return envToken
	}
	if envToken := strings.TrimSpace(os.Getenv("TELEGRAM_BOT_TOKEN")); envToken != "" {
		return envToken
	}
	if st == nil {
		return ""
	}
	setting, err := st.Settings().Get(ctx, "chatops.telegram.bot_token")
	if err == nil && len(setting.ValueJSON) > 0 {
		var botToken string
		if err := json.Unmarshal(setting.ValueJSON, &botToken); err == nil && strings.TrimSpace(botToken) != "" {
			return strings.TrimSpace(botToken)
		}
		raw := strings.Trim(string(setting.ValueJSON), "\"")
		if raw != "" && raw != "null" {
			return raw
		}
	}
	if chSetting, err := st.Settings().Get(ctx, "notify.telegram.token"); err == nil && len(chSetting.ValueJSON) > 0 {
		var botToken string
		if err := json.Unmarshal(chSetting.ValueJSON, &botToken); err == nil && strings.TrimSpace(botToken) != "" {
			return strings.TrimSpace(botToken)
		}
	}
	return ""
}

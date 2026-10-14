package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/chatops"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// 类型别名与包级变量，确保现有测试与调用方 100% 平滑兼容。
type ChatOpsActionData = chatops.ActionData
type ChatOpsActionExecutor = chatops.ActionExecutor
type GitHubChatOpsExecutor = chatops.GitHubChatOpsExecutor

var (
	CreateChatOpsToken             = chatops.CreateChatOpsToken
	CreateChatOpsTokenWithPayload  = chatops.CreateChatOpsTokenWithPayload
	ConsumeChatOpsToken            = chatops.ConsumeChatOpsToken
	ReleaseChatOpsToken            = chatops.ReleaseChatOpsToken
	readSecureConfig               = chatops.ReadSecureConfig
	maskToken                      = chatops.MaskToken
	resolveTelegramBotToken        = chatops.ResolveTelegramBotToken
	editTelegramMessageReplyMarkup = func(ctx context.Context, st store.Store, chatID int64, messageID int, clickedData, actionName string, keyboard [][]map[string]any) {
		fb := chatops.NewDefaultTelegramFeedback(st, nil)
		fb.EditMessageReplyMarkup(ctx, chatID, messageID, clickedData, actionName, keyboard)
	}
	answerTelegramCallback = func(ctx context.Context, st store.Store, callbackID, actionName string) {
		fb := chatops.NewDefaultTelegramFeedback(st, nil)
		fb.AnswerActionCallback(ctx, callbackID, actionName)
	}
	answerTelegramCustom = func(ctx context.Context, st store.Store, callbackID, text string, showAlert bool) {
		fb := chatops.NewDefaultTelegramFeedback(st, nil)
		fb.AnswerCallback(ctx, callbackID, text, showAlert)
	}
)

// handleTelegramChatOpsCallback 纯传输适配器：解析 Telegram Webhook 报文，委托 chatops.Service 处理。
func (s *server) handleTelegramChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	// 1. 传输层 Secret Token 校验
	if s.dependencies.Store != nil {
		expectedToken, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.telegram.secret_token")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops telegram secret token failed", "error", err)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "security_config_corrupted"})
			return
		}
		if expectedToken != "" {
			headerToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
			if subtle.ConstantTimeCompare([]byte(headerToken), []byte(expectedToken)) != 1 {
				writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
				return
			}
		}
	}

	body, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	var update struct {
		UpdateID      int `json:"update_id"`
		CallbackQuery *struct {
			ID   string `json:"id"`
			Data string `json:"data"`
			From struct {
				ID       int64  `json:"id"`
				Username string `json:"username"`
			} `json:"from"`
			Message *struct {
				MessageID int `json:"message_id"`
				Chat      struct {
					ID int64 `json:"id"`
				} `json:"chat"`
				ReplyMarkup *struct {
					InlineKeyboard [][]map[string]any `json:"inline_keyboard"`
				} `json:"reply_markup"`
			} `json:"message"`
		} `json:"callback_query"`
	}

	if err := json.Unmarshal(body, &update); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	if update.CallbackQuery == nil || update.CallbackQuery.Data == "" {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	cb := update.CallbackQuery
	var messageID int
	var chatID int64
	var inlineKeyboard [][]map[string]any
	if cb.Message != nil {
		messageID = cb.Message.MessageID
		chatID = cb.Message.Chat.ID
		if cb.Message.ReplyMarkup != nil {
			inlineKeyboard = cb.Message.ReplyMarkup.InlineKeyboard
		}
	}

	req := chatops.TelegramCallbackRequest{
		CallbackQueryID: cb.ID,
		Data:            cb.Data,
		FromID:          cb.From.ID,
		FromUsername:    cb.From.Username,
		MessageID:       messageID,
		ChatID:          chatID,
		InlineKeyboard:  inlineKeyboard,
	}

	outcome, err := s.chatOpsService.HandleTelegram(r.Context(), req)
	if err != nil {
		if errors.Is(err, chatops.ErrActorMismatch) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "forbidden", "error": "chatops_actor_forbidden"})
			return
		}
		if errors.Is(err, chatops.ErrExecutorUnavailable) {
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "service_unavailable", "error": "chatops_unavailable"})
			return
		}
		switch outcome.Status {
		case chatops.StatusCorruptedConfig:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "error", "error": "whitelist_corrupted"})
		case chatops.StatusForbidden:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "forbidden", "error": "telegram_user_not_allowed"})
		case chatops.StatusInvalidToken:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ignored", "reason": err.Error()})
		default:
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "action_failed", "error": "action_execution_failed"})
		}
		return
	}

	if outcome.Status == chatops.StatusAlreadyDone {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if outcome.Status == chatops.StatusPending {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "pending", "message": outcome.Message})
		return
	}
	if outcome.Status == chatops.StatusIndeterminate {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "indeterminate", "error": "status_indeterminate", "message": outcome.Message})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"ok":     true,
		"action": outcome.Action,
		"status": "success",
	})
}

// handleFeishuChatOpsCallback 纯传输适配器：解析飞书卡片互动事件，委托 chatops.Service 处理。
func (s *server) handleFeishuChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 1<<20))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_body"})
		return
	}

	var body map[string]any
	if err := json.Unmarshal(bodyBytes, &body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_json"})
		return
	}

	// 1. 校验 Verification Token
	if s.dependencies.Store != nil {
		expectedToken, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.feishu.verification_token")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops feishu verification token failed", "error", err)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "security_config_corrupted"})
			return
		}
		if expectedToken != "" {
			token, _ := body["token"].(string)
			if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
				return
			}
		}
	}

	// 2. URL Verification Challenge
	if msgType, ok := body["type"].(string); ok && msgType == "url_verification" {
		challenge, _ := body["challenge"].(string)
		writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}

	// 3. 提取 Token 与用户 ID
	var tokenID string
	if actionObj, ok := body["action"].(map[string]any); ok {
		if valObj, ok := actionObj["value"].(map[string]any); ok {
			if tok, ok := valObj["token"].(string); ok {
				tokenID = tok
			} else if tok, ok := valObj["chatops_token"].(string); ok {
				tokenID = tok
			}
		} else if valStr, ok := actionObj["value"].(string); ok {
			tokenID = strings.TrimPrefix(strings.TrimPrefix(valStr, "act:"), "rerun:")
		}
	}

	var operatorOpenID string
	if raw, ok := body["open_id"].(string); ok {
		operatorOpenID = strings.TrimSpace(raw)
	}
	if operator, ok := body["operator"].(map[string]any); ok {
		if raw, ok := operator["open_id"].(string); ok && strings.TrimSpace(raw) != "" {
			operatorOpenID = strings.TrimSpace(raw)
		}
	}

	req := chatops.FeishuCallbackRequest{
		TokenID:        tokenID,
		OperatorOpenID: operatorOpenID,
	}

	outcome, err := s.chatOpsService.HandleFeishu(r.Context(), req)
	if err != nil {
		if errors.Is(err, chatops.ErrExecutorUnavailable) {
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 执行器暂不可用",
				},
			})
			return
		}
		switch outcome.Status {
		case chatops.StatusCorruptedConfig:
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 安全配置异常，请联系管理员",
				},
			})
		case chatops.StatusForbidden:
			if errors.Is(err, chatops.ErrActorMismatch) {
				writeJSON(w, http.StatusOK, map[string]any{
					"toast": map[string]string{
						"type":    "danger",
						"content": "⚠️ 无权执行此操作",
					},
				})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{
					"toast": map[string]string{
						"type":    "warning",
						"content": "⚠️ 仅受信任的维护者有权执行此操作",
					},
				})
			}
		case chatops.StatusInvalidToken:
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "warning",
					"content": "Action expired or already performed",
				},
			})
		case chatops.StatusIndeterminate:
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 网络或远端超时，变更结果待确认，令牌已锁定",
				},
			})
		case chatops.StatusRetryableFailure:
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 操作执行失败，令牌已恢复，可稍后重试",
				},
			})
		default:
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 操作执行失败，令牌已恢复，可稍后重试",
				},
			})
		}
		return
	}

	if outcome.Status == chatops.StatusAlreadyDone {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "info",
				"content": "ℹ️ " + outcome.Message,
			},
		})
		return
	}
	if outcome.Status == chatops.StatusPending {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "warning",
				"content": "⏳ " + outcome.Message,
			},
		})
		return
	}
	if outcome.Status == chatops.StatusIndeterminate {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "danger",
				"content": "⚠️ " + outcome.Message,
			},
		})
		return
	}

	// 成功响应 toast 与卡片回写
	toastContent := "✅ " + outcome.Message
	writeJSON(w, http.StatusOK, map[string]any{
		"toast": map[string]string{
			"type":    "success",
			"content": toastContent,
		},
	})
}

// handleSlackChatOpsCallback 纯传输适配器：解析 Slack 交互报文，委托 chatops.Service 处理。
func (s *server) handleSlackChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request_too_large"})
		return
	}

	// 1. Slack HMAC 签名与时间戳重放校验
	if s.dependencies.Store != nil {
		signingSecret, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.slack.signing_secret")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops slack signing secret failed", "error", err)
			}
			writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "security_config_corrupted"})
			return
		}
		if signingSecret != "" {
			tsStr := r.Header.Get("X-Slack-Request-Timestamp")
			sigHeader := r.Header.Get("X-Slack-Signature")
			ts, tsErr := strconv.ParseInt(tsStr, 10, 64)
			if tsErr != nil || math.Abs(float64(time.Now().Unix()-ts)) > 300 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "timestamp_out_of_range"})
				return
			}
			mac := hmac.New(sha256.New, []byte(signingSecret))
			mac.Write([]byte(fmt.Sprintf("v0:%d:%s", ts, string(bodyBytes))))
			expectedSig := "v0=" + hex.EncodeToString(mac.Sum(nil))
			if subtle.ConstantTimeCompare([]byte(sigHeader), []byte(expectedSig)) != 1 {
				writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "invalid_signature"})
				return
			}
		}
	}

	vals, err := url.ParseQuery(string(bodyBytes))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_form"})
		return
	}
	payloadRaw := vals.Get("payload")
	if payloadRaw == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "missing_payload"})
		return
	}

	var payload struct {
		Type string `json:"type"`
		User struct {
			ID       string `json:"id"`
			Username string `json:"username"`
		} `json:"user"`
		Actions []struct {
			ActionID string `json:"action_id"`
			Value    string `json:"value"`
		} `json:"actions"`
		ResponseURL string `json:"response_url"`
	}
	if err := json.Unmarshal([]byte(payloadRaw), &payload); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid_payload_json"})
		return
	}

	if len(payload.Actions) == 0 || strings.TrimSpace(payload.Actions[0].Value) == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "未指定操作动作",
		})
		return
	}

	req := chatops.SlackCallbackRequest{
		TokenID:     payload.Actions[0].Value,
		UserID:      payload.User.ID,
		ResponseURL: payload.ResponseURL,
	}

	outcome, err := s.chatOpsService.HandleSlack(r.Context(), req)
	if err != nil {
		if errors.Is(err, chatops.ErrExecutorUnavailable) {
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 执行器暂不可用",
			})
			return
		}
		switch outcome.Status {
		case chatops.StatusCorruptedConfig:
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 安全配置异常，请联系管理员",
			})
		case chatops.StatusForbidden:
			if errors.Is(err, chatops.ErrActorMismatch) {
				writeJSON(w, http.StatusOK, map[string]any{
					"response_type": "ephemeral",
					"text":          "⚠️ 无权执行此操作",
				})
			} else {
				writeJSON(w, http.StatusOK, map[string]any{
					"response_type": "ephemeral",
					"text":          "⚠️ 仅受信任的维护者有权执行此操作",
				})
			}
		case chatops.StatusInvalidToken:
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 操作已失效或已被执行",
			})
		case chatops.StatusIndeterminate:
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 网络或远端超时，变更结果待确认，令牌已锁定",
			})
		case chatops.StatusRetryableFailure:
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 操作执行失败，请稍后重试",
			})
		default:
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 操作执行失败，请稍后重试",
			})
		}
		return
	}

	if outcome.Status == chatops.StatusAlreadyDone {
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "ℹ️ " + outcome.Message,
		})
		return
	}
	if outcome.Status == chatops.StatusPending {
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⏳ " + outcome.Message,
		})
		return
	}
	if outcome.Status == chatops.StatusIndeterminate {
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⚠️ " + outcome.Message,
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"response_type": "ephemeral",
		"text":          "✅ 操作已执行",
	})
}

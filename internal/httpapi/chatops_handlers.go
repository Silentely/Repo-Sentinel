package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func maskToken(token string) string {
	if len(token) <= 6 {
		return "***"
	}
	return token[:6] + "..."
}

// ChatOpsActionData represents the payload for an interactive button action.
type ChatOpsActionData = rules.ChatOpsActionData

// ChatOpsActionExecutor 执行已经完成校验与消费的 ChatOps 动作。
type ChatOpsActionExecutor interface {
	Execute(context.Context, ChatOpsActionData) error
}

// GitHubChatOpsExecutor 执行当前支持的 GitHub ChatOps 动作。
type GitHubChatOpsExecutor struct {
	Store  store.Store
	Client *githubx.AppClient
}

func (e *GitHubChatOpsExecutor) Execute(ctx context.Context, action ChatOpsActionData) error {
	if e == nil || e.Store == nil || e.Client == nil {
		return errors.New("chatops executor unavailable")
	}
	repo, err := e.Store.Repositories().Get(ctx, action.RepoID)
	if err != nil {
		return fmt.Errorf("load repository: %w", err)
	}
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) != 2 {
		return errors.New("invalid repository full name")
	}
	var token string
	if repo.InstallationID != nil && strings.TrimSpace(*repo.InstallationID) != "" {
		raw := strings.TrimSpace(*repo.InstallationID)
		var installationID int64
		if inst, err := e.Store.Installations().Get(ctx, raw); err == nil && inst.InstallationID > 0 {
			installationID = inst.InstallationID
		} else if id, err := strconv.ParseInt(raw, 10, 64); err == nil && id > 0 {
			if inst, err := e.Store.Installations().GetByInstallationID(ctx, id); err == nil && inst.InstallationID > 0 {
				installationID = inst.InstallationID
			} else {
				installationID = id
			}
		}
		if installationID <= 0 {
			return errors.New("invalid installation id")
		}
		token, err = e.Client.InstallationToken(ctx, installationID)
		if err != nil {
			return fmt.Errorf("load installation token: %w", err)
		}
	}
	var settingsStore store.SettingsStore
	if e.Store != nil {
		settingsStore = e.Store.Settings()
	}

	switch action.Action {
	case "issue_mark_duplicate":
		if action.IssueNumber <= 0 {
			return errors.New("invalid issue number")
		}
		dupTarget := strings.TrimSpace(action.Payload)
		if dupTarget == "" {
			return errors.New("empty duplicate target payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "issue_mark_duplicate", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			if closeErr := e.Client.CloseIssue(ctx, token, parts[0], parts[1], action.IssueNumber, "not_planned"); closeErr != nil {
				return "", closeErr
			}
			if labelErr := e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, []string{"duplicate"}); labelErr != nil {
				if !githubx.IsLabelNotFoundError(labelErr) {
					return "", fmt.Errorf("add duplicate label: %w", labelErr)
				}
			}
			comment := fmt.Sprintf("已识别该议题疑似与 #%s 相同，因此先行关联并关闭。如有其他疑问欢迎在此继续补充讨论。", dupTarget)
			if commentErr := e.Client.CreateIssueComment(ctx, token, parts[0], parts[1], action.IssueNumber, comment); commentErr != nil {
				return "", fmt.Errorf("create duplicate comment: %w", commentErr)
			}
			return scopeKey, nil
		})

	case "workflow_rerun":
		runID, err := strconv.ParseInt(strings.TrimSpace(action.RunID), 10, 64)
		if err != nil || runID <= 0 {
			return errors.New("invalid workflow run id")
		}
		return rules.ExecuteWithReceipt(ctx, settingsStore, "workflow_rerun", action.RepoID, action.RunID, "v1", strconv.FormatInt(runID, 10), func() (string, error) {
			err := e.Client.RerunWorkflow(ctx, token, parts[0], parts[1], runID)
			if err != nil {
				return "", err
			}
			return action.RunID, nil
		})

	case "issue_reply_draft":
		if action.IssueNumber <= 0 {
			return errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return errors.New("empty reply draft payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "issue_reply_draft", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			err := e.Client.CreateIssueComment(ctx, token, parts[0], parts[1], action.IssueNumber, action.Payload)
			if err != nil {
				return "", err
			}
			return scopeKey, nil
		})

	case "issue_mark_spam_and_close":
		if action.IssueNumber <= 0 {
			return errors.New("invalid issue number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "issue_mark_spam_and_close", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			err := e.Client.CloseIssue(ctx, token, parts[0], parts[1], action.IssueNumber, "not_planned")
			if err != nil {
				return "", err
			}
			if labelErr := e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, []string{"invalid"}); labelErr != nil {
				if !githubx.IsLabelNotFoundError(labelErr) {
					return "", fmt.Errorf("add spam label: %w", labelErr)
				}
			}
			return scopeKey, nil
		})

	case "issue_apply_labels":
		if action.IssueNumber <= 0 {
			return errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return errors.New("empty labels payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "issue_apply_labels", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			partsList := strings.Split(action.Payload, ",")
			var cleanLabels []string
			for _, l := range partsList {
				if trimmed := strings.TrimSpace(l); trimmed != "" {
					cleanLabels = append(cleanLabels, trimmed)
				}
			}
			if len(cleanLabels) == 0 {
				return "", errors.New("no valid labels to apply")
			}
			err := e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, cleanLabels)
			if err != nil {
				return "", err
			}
			return scopeKey, nil
		})

	case "issue_request_details":
		if action.IssueNumber <= 0 {
			return errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return errors.New("empty request details payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "issue_request_details", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			if labelErr := e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, []string{"needs-more-info"}); labelErr != nil {
				if !githubx.IsLabelNotFoundError(labelErr) {
					return "", fmt.Errorf("add needs-more-info label: %w", labelErr)
				}
			}
			err := e.Client.CreateIssueComment(ctx, token, parts[0], parts[1], action.IssueNumber, action.Payload)
			if err != nil {
				return "", err
			}
			return scopeKey, nil
		})

	case "issue_snooze", "pr_snooze":
		if action.IssueNumber <= 0 {
			return errors.New("invalid item number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, action.Action, action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			partsPayload := strings.Split(action.Payload, "|")
			repoFull := partsPayload[0]
			itemNum := action.IssueNumber
			link := ""
			if len(partsPayload) >= 3 {
				link = partsPayload[2]
			}
			channels, err := e.Store.Channels().List(ctx)
			if err != nil {
				return "", err
			}
			remindAt := time.Now().UTC().Add(2 * time.Hour)
			itemKind := store.WorkItemKindIssue
			kindLabel := "Issue"
			if action.Action == "pr_snooze" {
				itemKind = store.WorkItemKindPR
				kindLabel = "PR"
			}
			for _, ch := range channels {
				if !ch.Enabled || !ch.AcceptsKind(itemKind) {
					continue
				}
				remindTitle := fmt.Sprintf("⏰ [待办提醒] %s %s #%d", repoFull, kindLabel, itemNum)
				remindBody := fmt.Sprintf("<b>⏰ 稍后处理待办提醒</b>\n────────\n您在 2 小时前延迟了此 %s 的处理：\n仓库：<code>%s</code>\n%s：#%d", kindLabel, htmlpkg.EscapeString(repoFull), kindLabel, itemNum)
				if link != "" {
					remindBody += fmt.Sprintf("\n────────\n<a href=\"%s\">🔗 前往 GitHub</a>", htmlpkg.EscapeString(link))
				}
				snoozeIdem := fmt.Sprintf("snooze|%s|%s|%d|%s", ch.ID, action.Action, itemNum, action.ID)
				_, err := e.Store.Outbox().Create(ctx, store.NotificationOutbox{
					ID:             ulid.Make().String(),
					ChannelID:      ch.ID,
					IdempotencyKey: snoozeIdem,
					Status:         store.OutboxPending,
					NextAttemptAt:  remindAt,
					Title:          remindTitle,
					BodyText:       remindBody,
					HTMLURL:        link,
					ParseMode:      "HTML",
				})
				if err != nil && !errors.Is(err, store.ErrConflict) {
					return "", fmt.Errorf("create snooze outbox: %w", err)
				}
			}
			return scopeKey, nil
		})

	case "pr_quick_approve":
		if action.IssueNumber <= 0 {
			return errors.New("invalid pr number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		return rules.ExecuteWithReceipt(ctx, settingsStore, "pr_quick_approve", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			reviewMsg := "LGTM. Approved by maintainer via Repo-Sentinel."
			err := e.Client.CreatePullRequestReview(ctx, token, parts[0], parts[1], action.IssueNumber, "APPROVE", reviewMsg)
			if err != nil {
				return "", err
			}
			return scopeKey, nil
		})

	default:
		return fmt.Errorf("unsupported chatops action: %s", action.Action)
	}
}

// CreateChatOpsToken stores a single-use action token with a specified TTL.
func CreateChatOpsToken(ctx context.Context, st store.Store, action, repoID, runID, actorID string, ttl time.Duration) (string, error) {
	return rules.CreateChatOpsToken(ctx, st, action, repoID, runID, actorID, ttl)
}

// CreateChatOpsTokenWithPayload 创建包含扩展载荷（如 Issue 编号与草稿文本）的单次操作令牌。
func CreateChatOpsTokenWithPayload(ctx context.Context, st store.Store, action, repoID string, issueNumber int, payload, actorID string, ttl time.Duration) (string, error) {
	return rules.CreateChatOpsTokenWithPayload(ctx, st, action, repoID, issueNumber, payload, actorID, ttl)
}

// ConsumeChatOpsToken atomically retrieves and marks an action token as consumed.
func ConsumeChatOpsToken(ctx context.Context, st store.Store, tokenID string) (*ChatOpsActionData, error) {
	return rules.ConsumeChatOpsToken(ctx, st, tokenID)
}

// ReleaseChatOpsToken 仅用于动作执行失败时释放已领取的 Token，允许安全重试。
func ReleaseChatOpsToken(ctx context.Context, st store.Store, tokenID string) error {
	return rules.ReleaseChatOpsToken(ctx, st, tokenID)
}

// readSecureConfig 从 SettingsStore 安全读取敏感字符串配置（如密钥、凭证、白名单）。
// 严格践行 fail-closed 原则：
// - 若 key 不存在（store.ErrNotFound），返回 ("", nil) 表示未配置；
// - 若读取发生瞬时错误，返回错误；
// - 若 key 存在但数据为空字节、JSON 值为 null、非合法 JSON、反序列化结果为空白字符串，一律视为损坏并返回错误。
func readSecureConfig(ctx context.Context, st store.SettingsStore, key string) (string, error) {
	if st == nil {
		return "", nil
	}
	setting, err := st.Get(ctx, key)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return "", nil
		}
		return "", fmt.Errorf("read security config failed: %w", err)
	}
	trimmedRaw := bytes.TrimSpace(setting.ValueJSON)
	if len(trimmedRaw) == 0 || bytes.Equal(trimmedRaw, []byte("null")) {
		return "", fmt.Errorf("security config %q is empty or null", key)
	}
	var val string
	if err := json.Unmarshal(setting.ValueJSON, &val); err != nil {
		return "", fmt.Errorf("security config %q corrupted: %w", key, err)
	}
	val = strings.TrimSpace(val)
	if val == "" {
		return "", fmt.Errorf("security config %q is blank string", key)
	}
	return val, nil
}

// handleTelegramChatOpsCallback handles Telegram webhook callback queries.
func (s *server) handleTelegramChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	// 1. Verify Secret Token if configured
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

	data := update.CallbackQuery.Data
	if strings.HasPrefix(data, "done:") {
		if update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_done", func() {
				answerTelegramCallback(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, data)
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}
	if !strings.HasPrefix(data, "act:") && !strings.HasPrefix(data, "rerun:") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	tokenID := strings.TrimPrefix(strings.TrimPrefix(data, "act:"), "rerun:")

	// 1. 先验证是否属于授权的维护者用户（单兵安全边界，避免未授权用户抢占/消费令牌）
	if s.dependencies.Store != nil {
		allowedStr, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.telegram.allowed_user_ids")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops telegram whitelist failed", "error", err)
			}
			if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
				s.safeGo("telegram_callback_err", func() {
					answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 安全配置异常，请联系管理员", true)
				})
			}
			writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "error", "error": "whitelist_corrupted"})
			return
		}
		if allowedStr != "" {
			userID := strconv.FormatInt(update.CallbackQuery.From.ID, 10)
			allowed := false
			for _, uid := range strings.Split(allowedStr, ",") {
				if strings.TrimSpace(uid) == userID {
					allowed = true
					break
				}
			}
			if !allowed {
				if update.CallbackQuery.ID != "" {
					s.safeGo("telegram_callback_unauth", func() {
						answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 仅受信任的维护者有权执行此操作", true)
					})
				}
				writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "forbidden", "error": "telegram_user_not_allowed"})
				return
			}
		}
	}

	// 2. 白名单通过后，原子消费单次操作令牌
	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("telegram chatops token consumption failed", "token_prefix", maskToken(tokenID), "error", err.Error())
		}
		if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_invalid", func() {
				answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 操作已失效或已被执行", true)
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ignored", "reason": err.Error()})
		return
	}

	if action.ActorID != "" && strconv.FormatInt(update.CallbackQuery.From.ID, 10) != action.ActorID {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_forbidden", func() {
				answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 无权执行此操作", true)
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "forbidden", "error": "chatops_actor_forbidden"})
		return
	}
	if s.chatOpsExecutor == nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_unavailable", func() {
				answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 执行器暂不可用", true)
			})
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "service_unavailable", "error": "chatops_unavailable"})
		return
	}
	if err := s.chatOpsExecutor.Execute(r.Context(), *action); err != nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_failed", func() {
				answerTelegramCustom(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, "⚠️ 操作执行失败，令牌已恢复，可稍后重试", true)
			})
		}
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Error("telegram chatops action execution failed", "action", action.Action, "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "action_failed", "error": "action_execution_failed"})
		return
	}

	if update.CallbackQuery != nil {
		if update.CallbackQuery.ID != "" {
			s.safeGo("telegram_callback_action", func() {
				answerTelegramCallback(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, action.Action)
			})
		}
		if msg := update.CallbackQuery.Message; msg != nil && msg.ReplyMarkup != nil && len(msg.ReplyMarkup.InlineKeyboard) > 0 {
			s.safeGo("telegram_edit_markup", func() {
				editTelegramMessageReplyMarkup(context.Background(), s.dependencies.Store, msg.Chat.ID, msg.MessageID, data, action.Action, msg.ReplyMarkup.InlineKeyboard)
			})
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// handleFeishuChatOpsCallback handles Feishu interactive card events and URL verification.
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

	// 1. Verify Verification Token if configured (applies to challenge and interactive events)
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

	// 2. Handle URL Verification Challenge
	if msgType, ok := body["type"].(string); ok && msgType == "url_verification" {
		challenge, _ := body["challenge"].(string)
		writeJSON(w, http.StatusOK, map[string]string{"challenge": challenge})
		return
	}

	// 2. Handle Interactive Card Action
	// In Feishu, action value can be in body["action"].(map[string]any)["value"]
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

	if tokenID == "" {
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "info",
				"content": "未指定操作动作",
			},
		})
		return
	}

	// 1.5 验证是否属于授权的维护者用户（单兵安全边界，避免未授权用户抢占/消费令牌）
	if s.dependencies.Store != nil {
		allowedStr, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.feishu.allowed_user_ids")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops feishu whitelist failed", "error", err)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 安全配置异常，请联系管理员",
				},
			})
			return
		}
		if allowedStr != "" {
			userID := ""
			if raw, ok := body["open_id"].(string); ok {
				userID = strings.TrimSpace(raw)
			}
			if operator, ok := body["operator"].(map[string]any); ok {
				if raw, ok := operator["open_id"].(string); ok && strings.TrimSpace(raw) != "" {
					userID = strings.TrimSpace(raw)
				}
			}
			allowed := false
			for _, uid := range strings.Split(allowedStr, ",") {
				if strings.TrimSpace(uid) == userID {
					allowed = true
					break
				}
			}
			if !allowed {
				writeJSON(w, http.StatusOK, map[string]any{
					"toast": map[string]string{
						"type":    "warning",
						"content": "⚠️ 仅受信任的维护者有权执行此操作",
					},
				})
				return
			}
		}
	}

	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("feishu chatops token consumption failed", "token_prefix", maskToken(tokenID), "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "warning",
				"content": "Action expired or already performed",
			},
		})
		return
	}
	if action.ActorID != "" {
		actorID := ""
		if raw, ok := body["open_id"].(string); ok {
			actorID = strings.TrimSpace(raw)
		}
		if operator, ok := body["operator"].(map[string]any); ok {
			if raw, ok := operator["open_id"].(string); ok && strings.TrimSpace(raw) != "" {
				actorID = strings.TrimSpace(raw)
			}
		}
		if actorID == "" || actorID != action.ActorID {
			_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
			writeJSON(w, http.StatusOK, map[string]any{
				"toast": map[string]string{
					"type":    "danger",
					"content": "⚠️ 无权执行此操作",
				},
			})
			return
		}
	}

	if s.chatOpsExecutor == nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "danger",
				"content": "⚠️ 执行器暂不可用",
			},
		})
		return
	}
	if err := s.chatOpsExecutor.Execute(r.Context(), *action); err != nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusOK, map[string]any{
			"toast": map[string]string{
				"type":    "danger",
				"content": "⚠️ 操作执行失败，令牌已恢复，可稍后重试",
			},
		})
		return
	}

	var toastText string
	switch action.Action {
	case "issue_mark_duplicate":
		toastText = "🔗 已在 GitHub 关联历史议题并关闭"
	case "issue_reply_draft":
		toastText = "✅ 已在 GitHub 成功发布建议回复"
	case "issue_mark_spam_and_close":
		toastText = "🚫 已在 GitHub 标记垃圾内容并关闭"
	case "issue_apply_labels":
		toastText = "🏷️ 已在 GitHub 成功应用建议标签"
	case "issue_request_details":
		toastText = "📋 已在 GitHub 发布要素补充追问并打标"
	case "issue_snooze", "pr_snooze":
		toastText = "💤 已设置 2 小时后重新提醒"
	case "pr_quick_approve":
		toastText = "✅ 已在 GitHub 成功批准 PR 合入"
	case "workflow_rerun":
		toastText = "🔄 已触发重新运行工作流"
	default:
		toastText = "✅ 操作已成功完成"
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"toast": map[string]string{
			"type":    "success",
			"content": toastText,
		},
	})
}

// resolveTelegramBotToken 解析 Telegram Bot Token：优先从环境变量或系统设置获取。
func resolveTelegramBotToken(ctx context.Context, st store.Store) string {
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
	}
	if chSetting, err := st.Settings().Get(ctx, "notify.telegram.token"); err == nil && len(chSetting.ValueJSON) > 0 {
		var botToken string
		if err := json.Unmarshal(chSetting.ValueJSON, &botToken); err == nil && strings.TrimSpace(botToken) != "" {
			return strings.TrimSpace(botToken)
		}
	}
	return ""
}

func answerTelegramCallback(ctx context.Context, st store.Store, callbackID, actionName string) {
	if callbackID == "" || st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	botToken := resolveTelegramBotToken(ctx, st)
	if botToken == "" {
		return
	}
	var text string
	if strings.HasPrefix(actionName, "done:") {
		answerTelegramCustom(ctx, st, callbackID, "ℹ️ 该操作已完成，无需重复操作", false)
		return
	}
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
	payload := map[string]any{
		"callback_query_id": callbackID,
		"text":              text,
		"show_alert":        false,
	}
	raw, _ := json.Marshal(payload)
	url := fmt.Sprintf("https://api.telegram.org/bot%s/answerCallbackQuery", botToken)
	if strings.HasPrefix(botToken, "http://") || strings.HasPrefix(botToken, "https://") {
		url = botToken + "/answerCallbackQuery"
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

func editTelegramMessageReplyMarkup(ctx context.Context, st store.Store, chatID int64, messageID int, clickedData, actionName string, keyboard [][]map[string]any) {
	if chatID == 0 || messageID == 0 || st == nil || len(keyboard) == 0 {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	botToken := resolveTelegramBotToken(ctx, st)
	if botToken == "" {
		return
	}

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

	var newKeyboard [][]map[string]any
	if actionName == "issue_mark_spam_and_close" || actionName == "issue_mark_duplicate" {
		// 标记垃圾并关闭后，将键盘收敛为单一完成态按钮，避免维护者误触已关闭 Issue 的其他操作
		newKeyboard = [][]map[string]any{
			{
				{"text": statusLabel, "callback_data": "done:" + actionName},
			},
		}
	} else {
		for _, row := range keyboard {
			var newRow []map[string]any
			for _, btn := range row {
				btnCopy := make(map[string]any)
				for k, v := range btn {
					btnCopy[k] = v
				}
				if data, ok := btnCopy["callback_data"].(string); ok && data == clickedData {
					btnCopy["text"] = statusLabel
					btnCopy["callback_data"] = "done:" + actionName
				}
				newRow = append(newRow, btnCopy)
			}
			if len(newRow) > 0 {
				newKeyboard = append(newKeyboard, newRow)
			}
		}
	}

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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

func answerTelegramCustom(ctx context.Context, st store.Store, callbackID, text string, showAlert bool) {
	if callbackID == "" || st == nil {
		return
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	botToken := resolveTelegramBotToken(ctx, st)
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
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(raw))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err == nil && resp != nil {
		_ = resp.Body.Close()
	}
}

// handleSlackChatOpsCallback 处理 Slack Block Kit 交互动作回调。
func (s *server) handleSlackChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	bodyBytes, err := io.ReadAll(http.MaxBytesReader(w, r.Body, 64<<10))
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "request_too_large"})
		return
	}

	// 1. 如果配置了 chatops.slack.signing_secret，进行签名与时戳防重放校验
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

	// 2. 解析 Slack 发送的 application/x-www-form-urlencoded 表单
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
	tokenID := strings.TrimSpace(payload.Actions[0].Value)
	userID := strings.TrimSpace(payload.User.ID)

	// 3. 校验 Slack 用户白名单（若配置）
	if s.dependencies.Store != nil {
		allowedStr, err := readSecureConfig(r.Context(), s.dependencies.Store.Settings(), "chatops.slack.allowed_user_ids")
		if err != nil {
			if s.dependencies.Logger != nil {
				s.dependencies.Logger.Error("chatops slack whitelist failed", "error", err)
			}
			writeJSON(w, http.StatusOK, map[string]any{
				"response_type": "ephemeral",
				"text":          "⚠️ 安全配置异常，请联系管理员",
			})
			return
		}
		if allowedStr != "" {
			allowed := false
			for _, uid := range strings.Split(allowedStr, ",") {
				if strings.TrimSpace(uid) == userID {
					allowed = true
					break
				}
			}
			if !allowed {
				writeJSON(w, http.StatusOK, map[string]any{
					"response_type": "ephemeral",
					"text":          "⚠️ 仅受信任的维护者有权执行此操作",
				})
				return
			}
		}
	}

	// 4. 原子消费操作令牌
	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("slack chatops token consumption failed", "token_prefix", maskToken(tokenID), "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⚠️ 操作已失效或已被执行",
		})
		return
	}

	// 5. 校验操作者身份（ActorID）
	if action.ActorID != "" && (userID == "" || userID != action.ActorID) {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⚠️ 无权执行此操作",
		})
		return
	}

	if s.chatOpsExecutor == nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⚠️ 执行器暂不可用",
		})
		return
	}

	// 6. 执行操作动作
	if err := s.chatOpsExecutor.Execute(r.Context(), *action); err != nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Error("slack chatops action execution failed", "action", action.Action, "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{
			"response_type": "ephemeral",
			"text":          "⚠️ 操作执行失败，请稍后重试",
		})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"response_type": "ephemeral",
		"text":          "✅ 操作已执行",
	})
}

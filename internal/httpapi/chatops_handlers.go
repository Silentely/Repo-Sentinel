package httpapi

import (
	"bytes"
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

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
			_ = e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, []string{"invalid"})
			err := e.Client.CloseIssue(ctx, token, parts[0], parts[1], action.IssueNumber, "not_planned")
			if err != nil {
				return "", err
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
				return scopeKey, nil
			}
			err := e.Client.AddIssueLabels(ctx, token, parts[0], parts[1], action.IssueNumber, cleanLabels)
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

// handleTelegramChatOpsCallback handles Telegram webhook callback queries.
func (s *server) handleTelegramChatOpsCallback(w http.ResponseWriter, r *http.Request) {
	// 1. Verify Secret Token if configured
	if s.dependencies.Store != nil {
		setting, err := s.dependencies.Store.Settings().Get(r.Context(), "chatops.telegram.secret_token")
		if err == nil && len(setting.ValueJSON) > 0 {
			var expectedToken string
			if err := json.Unmarshal(setting.ValueJSON, &expectedToken); err == nil && expectedToken != "" {
				headerToken := r.Header.Get("X-Telegram-Bot-Api-Secret-Token")
				if subtle.ConstantTimeCompare([]byte(headerToken), []byte(expectedToken)) != 1 {
					writeJSON(w, http.StatusForbidden, map[string]string{"error": "forbidden"})
					return
				}
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
	if !strings.HasPrefix(data, "act:") && !strings.HasPrefix(data, "rerun:") {
		writeJSON(w, http.StatusOK, map[string]any{"ok": true})
		return
	}

	tokenID := strings.TrimPrefix(strings.TrimPrefix(data, "act:"), "rerun:")
	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("telegram chatops token consumption failed", "token_id", tokenID, "error", err.Error())
		}
		writeJSON(w, http.StatusOK, map[string]any{"ok": true, "status": "ignored", "reason": err.Error()})
		return
	}
	if action.ActorID != "" && strconv.FormatInt(update.CallbackQuery.From.ID, 10) != action.ActorID {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "chatops_actor_forbidden"})
		return
	}
	if s.chatOpsExecutor == nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chatops_unavailable"})
		return
	}
	if err := s.chatOpsExecutor.Execute(r.Context(), *action); err != nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "chatops_action_failed"})
		return
	}

	if update.CallbackQuery != nil && update.CallbackQuery.ID != "" {
		go answerTelegramCallback(context.Background(), s.dependencies.Store, update.CallbackQuery.ID, action.Action)
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
		setting, err := s.dependencies.Store.Settings().Get(r.Context(), "chatops.feishu.verification_token")
		if err == nil && len(setting.ValueJSON) > 0 {
			var expectedToken string
			if err := json.Unmarshal(setting.ValueJSON, &expectedToken); err == nil && expectedToken != "" {
				token, _ := body["token"].(string)
				if subtle.ConstantTimeCompare([]byte(token), []byte(expectedToken)) != 1 {
					writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unauthorized"})
					return
				}
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
				"content": "No action specified",
			},
		})
		return
	}

	action, err := ConsumeChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
	if err != nil {
		if s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("feishu chatops token consumption failed", "token_id", tokenID, "error", err.Error())
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
			actorID = raw
		}
		if operator, ok := body["operator"].(map[string]any); ok {
			if raw, ok := operator["open_id"].(string); ok {
				actorID = raw
			}
		}
		if actorID != "" && actorID != action.ActorID {
			_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
			writeJSON(w, http.StatusForbidden, map[string]string{"error": "chatops_actor_forbidden"})
			return
		}
	}

	if s.chatOpsExecutor == nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusServiceUnavailable, map[string]string{"error": "chatops_unavailable"})
		return
	}
	if err := s.chatOpsExecutor.Execute(r.Context(), *action); err != nil {
		_ = ReleaseChatOpsToken(r.Context(), s.dependencies.Store, tokenID)
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": "chatops_action_failed"})
		return
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"toast": map[string]string{
			"type":    "success",
			"content": "Action queued successfully",
		},
	})
}

func answerTelegramCallback(ctx context.Context, st store.Store, callbackID, actionName string) {
	if callbackID == "" || st == nil {
		return
	}
	setting, err := st.Settings().Get(ctx, "chatops.telegram.bot_token")
	if err != nil || len(setting.ValueJSON) == 0 {
		return
	}
	var botToken string
	if err := json.Unmarshal(setting.ValueJSON, &botToken); err != nil || botToken == "" {
		return
	}
	var text string
	switch actionName {
	case "issue_reply_draft":
		text = "✅ 已成功在 GitHub 发布建议回复"
	case "issue_mark_spam_and_close":
		text = "🚫 已在 GitHub 标记垃圾内容并关闭"
	case "issue_apply_labels":
		text = "🏷️ 已成功在 GitHub 应用建议标签"
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

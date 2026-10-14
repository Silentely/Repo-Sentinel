package chatops

import (
	"context"
	"errors"
	"fmt"
	htmlpkg "html"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/sideeffect"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ActionExecutor 执行已完成校验与领取的 ChatOps 动作。
type ActionExecutor interface {
	Execute(context.Context, ActionData) error
}

// OutcomeExecutor 是支持输出细粒度 Outcome 的执行器扩展契约。
type OutcomeExecutor interface {
	ActionExecutor
	ExecuteWithOutcome(context.Context, ActionData) (*Outcome, error)
}

// GitHubChatOpsExecutor 执行当前支持的 GitHub ChatOps 动作。
type GitHubChatOpsExecutor struct {
	Store  store.Store
	Client *githubx.AppClient
}

// Execute 执行动作，保留原始错误类型供外部调用方处理。
func (e *GitHubChatOpsExecutor) Execute(ctx context.Context, action ActionData) error {
	outcome, err := e.ExecuteWithOutcome(ctx, action)
	if err != nil {
		return err
	}
	if outcome != nil && outcome.Err != nil {
		return outcome.Err
	}
	return nil
}

// ExecuteWithOutcome 执行动作并返回结构化的领域 Outcome，明确副作用状态与释放准则。
func (e *GitHubChatOpsExecutor) ExecuteWithOutcome(ctx context.Context, action ActionData) (*Outcome, error) {
	if e == nil || e.Store == nil || e.Client == nil {
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "chatops executor unavailable",
			Err:     ErrExecutorUnavailable,
		}, ErrExecutorUnavailable
	}
	repo, err := e.Store.Repositories().Get(ctx, action.RepoID)
	if err != nil {
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "加载仓库信息失败",
			Err:     fmt.Errorf("load repository: %w", err),
		}, fmt.Errorf("load repository: %w", err)
	}
	parts := strings.SplitN(repo.FullName, "/", 2)
	if len(parts) != 2 {
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "仓库全名格式非法",
			Err:     errors.New("invalid repository full name"),
		}, errors.New("invalid repository full name")
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
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 GitHub App 安装凭据",
				Err:     errors.New("invalid installation id"),
			}, errors.New("invalid installation id")
		}
		token, err = e.Client.InstallationToken(ctx, installationID)
		if err != nil {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "获取 GitHub App 访问令牌失败",
				Err:     fmt.Errorf("load installation token: %w", err),
			}, fmt.Errorf("load installation token: %w", err)
		}
	}

	var settingsStore store.SettingsStore
	if e.Store != nil {
		settingsStore = e.Store.Settings()
	}

	switch action.Action {
	case "issue_mark_duplicate":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Issue 编号",
				Err:     errors.New("invalid issue number"),
			}, errors.New("invalid issue number")
		}
		dupTarget := strings.TrimSpace(action.Payload)
		if dupTarget == "" {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "缺失重复关联目标",
				Err:     errors.New("empty duplicate target payload"),
			}, errors.New("empty duplicate target payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "issue_mark_duplicate", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
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
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已关联 #%s 并关闭 Issue #%d", dupTarget, action.IssueNumber), scopeKey, detail, execErr)

	case "workflow_rerun":
		runID, err := strconv.ParseInt(strings.TrimSpace(action.RunID), 10, 64)
		if err != nil || runID <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Workflow Run ID",
				Err:     errors.New("invalid workflow run id"),
			}, errors.New("invalid workflow run id")
		}
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "workflow_rerun", action.RepoID, action.RunID, "v1", strconv.FormatInt(runID, 10), func() (string, error) {
			if rerunErr := e.Client.RerunWorkflow(ctx, token, parts[0], parts[1], runID); rerunErr != nil {
				return "", rerunErr
			}
			return strconv.FormatInt(runID, 10), nil
		})
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已成功触发 Workflow #%d 重新运行", runID), action.RunID, detail, execErr)

	case "issue_reply_draft":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Issue 编号",
				Err:     errors.New("invalid issue number"),
			}, errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "草稿回复内容为空",
				Err:     errors.New("empty reply draft payload"),
			}, errors.New("empty reply draft payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "issue_reply_draft", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			if commentErr := e.Client.CreateIssueComment(ctx, token, parts[0], parts[1], action.IssueNumber, action.Payload); commentErr != nil {
				return "", commentErr
			}
			return scopeKey, nil
		})
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已在 GitHub 成功发布 Issue #%d 建议回复", action.IssueNumber), scopeKey, detail, execErr)

	case "issue_mark_spam_and_close":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Issue 编号",
				Err:     errors.New("invalid issue number"),
			}, errors.New("invalid issue number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "issue_mark_spam_and_close", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
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
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已将 Issue #%d 标记为垃圾内容并关闭", action.IssueNumber), scopeKey, detail, execErr)

	case "issue_apply_labels":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Issue 编号",
				Err:     errors.New("invalid issue number"),
			}, errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "缺失待应用的标签",
				Err:     errors.New("empty labels payload"),
			}, errors.New("empty labels payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "issue_apply_labels", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
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
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已为 Issue #%d 成功应用标签", action.IssueNumber), scopeKey, detail, execErr)

	case "issue_request_details":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 Issue 编号",
				Err:     errors.New("invalid issue number"),
			}, errors.New("invalid issue number")
		}
		if strings.TrimSpace(action.Payload) == "" {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "缺失要素索取内容",
				Err:     errors.New("empty request details payload"),
			}, errors.New("empty request details payload")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "issue_request_details", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
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
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已为 Issue #%d 发送要素索取评论", action.IssueNumber), scopeKey, detail, execErr)

	case "issue_snooze", "pr_snooze":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的工作项编号",
				Err:     errors.New("invalid item number"),
			}, errors.New("invalid item number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, action.Action, action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
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
		targetName := "Issue"
		if action.Action == "pr_snooze" {
			targetName = "PR"
		}
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已为 %s #%d 延期提醒 2 小时", targetName, action.IssueNumber), scopeKey, detail, execErr)

	case "pr_quick_approve":
		if action.IssueNumber <= 0 {
			return &Outcome{
				Status:  StatusRetryableFailure,
				Action:  action.Action,
				Message: "无效的 PR 编号",
				Err:     errors.New("invalid pr number"),
			}, errors.New("invalid pr number")
		}
		scopeKey := fmt.Sprintf("%d:%s", action.IssueNumber, action.ID)
		detail, execErr := sideeffect.ExecuteWithReceiptDetail(ctx, settingsStore, "pr_quick_approve", action.RepoID, scopeKey, "v1", scopeKey, func() (string, error) {
			reviewMsg := "LGTM. Approved by maintainer via Repo-Sentinel."
			err := e.Client.CreatePullRequestReview(ctx, token, parts[0], parts[1], action.IssueNumber, "APPROVE", reviewMsg)
			if err != nil {
				return "", err
			}
			return scopeKey, nil
		})
		return e.mapReceiptDetailOutcome(action.Action, fmt.Sprintf("已成功快捷批准 PR #%d 合入", action.IssueNumber), scopeKey, detail, execErr)

	default:
		return &Outcome{
			Status:  StatusRetryableFailure,
			Action:  action.Action,
			Message: "不支持的 ChatOps 动作",
			Err:     fmt.Errorf("unsupported chatops action: %s", action.Action),
		}, fmt.Errorf("unsupported chatops action: %s", action.Action)
	}
}

func (e *GitHubChatOpsExecutor) mapReceiptOutcome(actionName, successMsg, externalID string, err error) (*Outcome, error) {
	if err == nil {
		return &Outcome{
			Status:     StatusExecuted,
			Action:     actionName,
			Message:    successMsg,
			ExternalID: externalID,
		}, nil
	}
	if sideeffect.IsRemoteSucceededReceiptFailed(err) ||
		sideeffect.IsNetworkUnknownError(err) ||
		strings.Contains(strings.ToLower(err.Error()), "succeeded externally but receipt persist failed") ||
		strings.Contains(strings.ToLower(err.Error()), "unknown-receipt persist failed") ||
		strings.Contains(strings.ToLower(err.Error()), "network unknown") {
		return &Outcome{
			Status:     StatusIndeterminate,
			Action:     actionName,
			Message:    "网络波动或远端超时，变更结果待确认",
			ExternalID: externalID,
			Err:        err,
		}, err
	}
	if sideeffect.IsPreSendError(err) {
		return &Outcome{
			Status:     StatusRetryableFailure,
			Action:     actionName,
			Message:    "参数校验失败，允许修正后重试",
			ExternalID: externalID,
			Err:        err,
		}, err
	}
	return &Outcome{
		Status:     StatusRetryableFailure,
		Action:     actionName,
		Message:    fmt.Sprintf("操作失败: %s", htmlpkg.EscapeString(err.Error())),
		ExternalID: externalID,
		Err:        err,
	}, err
}

func (e *GitHubChatOpsExecutor) mapReceiptDetailOutcome(actionName, successMsg, defaultExternalID string, detail *sideeffect.ExecutionDetail, err error) (*Outcome, error) {
	if err == nil {
		if detail != nil {
			switch detail.State {
			case sideeffect.ReceiptSucceeded:
				if !detail.IsNew {
					return &Outcome{
						Status:     StatusAlreadyDone,
						Action:     actionName,
						Message:    "操作此前已执行完成，跳过重复操作",
						ExternalID: detail.ExternalID,
					}, nil
				}
				extID := detail.ExternalID
				if extID == "" {
					extID = defaultExternalID
				}
				return &Outcome{
					Status:     StatusExecuted,
					Action:     actionName,
					Message:    successMsg,
					ExternalID: extID,
				}, nil
			case sideeffect.ReceiptPending:
				return &Outcome{
					Status:     StatusPending,
					Action:     actionName,
					Message:    "该动作已在后台处理中，请勿重复操作",
					ExternalID: detail.ExternalID,
				}, nil
			case sideeffect.ReceiptUnknown:
				return &Outcome{
					Status:     StatusIndeterminate,
					Action:     actionName,
					Message:    "该动作此前执行处于网络不确定状态，已锁定以防重复执行",
					ExternalID: detail.ExternalID,
				}, nil
			}
		}
		return &Outcome{
			Status:     StatusExecuted,
			Action:     actionName,
			Message:    successMsg,
			ExternalID: defaultExternalID,
		}, nil
	}

	// 关键防护：即使 err != nil，若 detail 显式处于 ReceiptUnknown，一律归入 StatusIndeterminate 锁定 Token
	if detail != nil && detail.State == sideeffect.ReceiptUnknown {
		return &Outcome{
			Status:     StatusIndeterminate,
			Action:     actionName,
			Message:    "网络波动或远端超时，变更结果待确认",
			ExternalID: defaultExternalID,
			Err:        err,
		}, err
	}

	return e.mapReceiptOutcome(actionName, successMsg, defaultExternalID, err)
}

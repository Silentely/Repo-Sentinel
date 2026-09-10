package webhooksvc

import (
	"context"
	"encoding/json"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ghPRPayload 用于提取 pull_request 事件的核心元数据与 diff。
type ghPRPayload struct {
	Action      string `json:"action"`
	Number      int    `json:"number"`
	PullRequest struct {
		Number int    `json:"number"`
		Title  string `json:"title"`
		User   struct {
			Login string `json:"login"`
		} `json:"user"`
		Draft  bool `json:"draft"`
		Merged bool `json:"merged"`
	} `json:"pull_request"`
	Repository struct {
		Owner struct {
			Login string `json:"login"`
		} `json:"owner"`
		Name     string `json:"name"`
		FullName string `json:"full_name"`
	} `json:"repository"`
	Installation *struct {
		ID int64 `json:"id"`
	} `json:"installation"`
}

// maybeTriggerAICodeReview 检查是否满足 PR AI 审查触发条件（opened 或 synchronize），并异步执行审查与可选评论回写。
func (s *Service) maybeTriggerAICodeReview(res normalizer.Result, body []byte) {
	if s.AI == nil || !s.AI.IsCodeReviewEnabled() {
		return
	}
	if res.Event == nil || res.Event.Kind != store.WorkItemKindPR {
		return
	}

	var payload ghPRPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		return
	}

	action := strings.ToLower(strings.TrimSpace(payload.Action))
	// 仅在 PR 打开 (opened) 或更新 commit (synchronize) 时触发审查
	if action != "opened" && action != "synchronize" {
		return
	}
	// 草稿 PR 不做主动审查
	if payload.PullRequest.Draft {
		return
	}

	owner := payload.Repository.Owner.Login
	repo := payload.Repository.Name
	if owner == "" || repo == "" {
		parts := strings.Split(payload.Repository.FullName, "/")
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}
	}
	prNum := payload.PullRequest.Number
	if prNum == 0 {
		prNum = payload.Number
	}
	if owner == "" || repo == "" || prNum == 0 {
		return
	}

	bgCtx := s.Background
	if bgCtx == nil {
		bgCtx = context.Background()
	}

	// 启动独立 goroutine 执行 Diff 拉取与 LLM 审计，不阻塞 Webhook 投递确认
	go func() {
		ctx, cancel := context.WithTimeout(context.WithoutCancel(bgCtx), 2*time.Minute)
		defer cancel()

		s.runAICodeReview(ctx, owner, repo, prNum, payload)
	}()
}

func (s *Service) runAICodeReview(ctx context.Context, owner, repo string, prNum int, payload ghPRPayload) {
	var token string
	if s.GitHub != nil && payload.Installation != nil && payload.Installation.ID != 0 {
		tok, err := s.GitHub.InstallationToken(ctx, payload.Installation.ID)
		if err == nil {
			token = tok
		} else if s.Logger != nil {
			s.Logger.Warn("ai code review: resolve installation token failed", "repo", payload.Repository.FullName, "pr", prNum, "error", err)
		}
	}

	// 1. 若无 AppClient，或者 token 为空，尝试通过 GitHub.GetPRDiff 获取 Diff
	var diff string
	if s.GitHub != nil {
		d, err := s.GitHub.GetPRDiff(ctx, token, owner, repo, prNum)
		if err == nil {
			diff = d
		} else if s.Logger != nil {
			s.Logger.Warn("ai code review: get pr diff failed", "repo", payload.Repository.FullName, "pr", prNum, "error", err)
		}
	}

	if strings.TrimSpace(diff) == "" {
		return
	}

	// 2. 调用 AI 审查
	reviewRes, err := s.AI.ReviewPR(ctx, payload.Repository.FullName, payload.PullRequest.Title, payload.PullRequest.User.Login, diff)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("ai code review: review pr failed", "repo", payload.Repository.FullName, "pr", prNum, "error", err)
		}
		return
	}

	// 3. 可选模式 B：若配置了发表评论且拥有 GitHub 权限，发表评论至 PR
	if s.AI.ShouldCommentOnPR() && s.GitHub != nil && token != "" {
		commentMD := ai.FormatPRComment(reviewRes)
		err := s.GitHub.CreateIssueComment(ctx, token, owner, repo, prNum, commentMD)
		if err == nil {
			reviewRes.CommentedOnPR = true
			if s.Logger != nil {
				s.Logger.Info("ai code review: commented on pr", "repo", payload.Repository.FullName, "pr", prNum)
			}
		} else {
			// 若由于无 write 权限（如 403）或网络问题失败，优雅降级，仅记录日志
			if s.Logger != nil {
				s.Logger.Warn("ai code review: comment on pr failed (graceful degradation)", "repo", payload.Repository.FullName, "pr", prNum, "error", err)
			}
		}
	}

	// 4. 模式 A：持久化审查结果到系统设置表中（以仓库ID+PR编号为键，后台控制台可实时获取）
	if s.Store != nil {
		// 查找该 work_item 并存入系统设置表中
		repoID := ""
		if repoRec, err := s.Store.Repositories().GetByFullName(ctx, payload.Repository.FullName); err == nil {
			repoID = repoRec.ID
		}
		if repoID != "" {
			item, err := s.Store.WorkItems().GetByRepoNumber(ctx, repoID, prNum)
			if err == nil {
				raw, _ := json.Marshal(reviewRes)
				settingKey := "ai.pr_review." + item.ID
				_, _ = s.Store.Settings().Upsert(ctx, store.SystemSetting{
					ID:        ulid.Make().String(),
					Key:       settingKey,
					ValueJSON: raw,
					UpdatedAt: time.Now().UTC(),
					UpdatedBy: "ai_code_review",
				})
			}
		}
	}
}

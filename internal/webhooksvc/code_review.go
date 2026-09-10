package webhooksvc

import (
	"context"
	"encoding/json"
	"fmt"
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
		Head   struct {
			SHA string `json:"sha"`
		} `json:"head"`
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
		parts := strings.SplitN(payload.Repository.FullName, "/", 2)
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
	if payload.Repository.FullName == "" {
		payload.Repository.FullName = owner + "/" + repo
	}

	bgCtx := s.Background
	if bgCtx == nil {
		bgCtx = context.Background()
	}

	// 同一 PR 同一提交只允许一个审查任务在途，避免 webhook 重试重复消耗 AI 配额。
	key := fmt.Sprintf("%s#%d#%s", owner+"/"+repo, prNum, payload.PullRequest.Head.SHA)
	s.reviewMu.Lock()
	if s.reviewInFlight == nil {
		s.reviewInFlight = make(map[string]struct{})
	}
	if _, exists := s.reviewInFlight[key]; exists {
		s.reviewMu.Unlock()
		return
	}
	s.reviewInFlight[key] = struct{}{}
	s.reviewMu.Unlock()

	// 启动独立 goroutine 执行 Diff 拉取与 LLM 审计，不阻塞 Webhook 投递确认
	go func() {
		defer func() {
			s.reviewMu.Lock()
			delete(s.reviewInFlight, key)
			s.reviewMu.Unlock()
			if recovered := recover(); recovered != nil && s.Logger != nil {
				s.Logger.Error("ai code review panic recovered", "repo", payload.Repository.FullName, "pr", prNum, "error", recovered)
			}
		}()
		reviewBudget := 2 * time.Minute
		if configured := s.AI.EffectiveTimeout() + webhookProcessMargin; configured > reviewBudget {
			reviewBudget = configured
		}
		ctx, cancel := context.WithTimeout(context.WithoutCancel(bgCtx), reviewBudget)
		defer cancel()

		s.runAICodeReview(ctx, owner, repo, prNum, payload)
	}()
}

func (s *Service) runAICodeReview(ctx context.Context, owner, repo string, prNum int, payload ghPRPayload) {
	if s.reviewAlreadyStored(ctx, payload.Repository.FullName, prNum, payload.PullRequest.Head.SHA) {
		return
	}
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
	reviewRes.HeadSHA = payload.PullRequest.Head.SHA

	// 3. 先持久化审查结果，评论回写失败时管理台仍可查看报告。
	if s.Store == nil {
		return
	}
	repoRec, err := s.Store.Repositories().GetByFullName(ctx, payload.Repository.FullName)
	if err != nil {
		return
	}
	item, err := s.Store.WorkItems().GetByRepoNumber(ctx, repoRec.ID, prNum)
	if err != nil {
		return
	}
	settingKey := "ai.pr_review." + item.ID
	persist := func() bool {
		raw, marshalErr := json.Marshal(reviewRes)
		if marshalErr != nil {
			return false
		}
		if _, saveErr := s.Store.Settings().Upsert(ctx, store.SystemSetting{
			ID:        ulid.Make().String(),
			Key:       settingKey,
			ValueJSON: raw,
			UpdatedAt: time.Now().UTC(),
			UpdatedBy: "ai_code_review",
		}); saveErr != nil {
			if s.Logger != nil {
				s.Logger.Warn("ai code review: persist result failed", "repo", payload.Repository.FullName, "pr", prNum, "error", saveErr)
			}
			return false
		}
		return true
	}
	if !persist() {
		return
	}

	// 4. 可选模式 B：持久化成功后再发表评论，避免外部评论先于内部状态存在。
	if s.AI.ShouldCommentOnPR() && s.GitHub != nil && token != "" {
		commentMD := ai.FormatPRComment(reviewRes)
		if err := s.GitHub.CreateIssueComment(ctx, token, owner, repo, prNum, commentMD); err != nil {
			if s.Logger != nil {
				s.Logger.Warn("ai code review: comment on pr failed", "repo", payload.Repository.FullName, "pr", prNum, "error", err)
			}
		} else {
			reviewRes.CommentedOnPR = true
			if !persist() && s.Logger != nil {
				s.Logger.Warn("ai code review: persist comment status failed", "repo", payload.Repository.FullName, "pr", prNum)
			}
			if s.Logger != nil {
				s.Logger.Info("ai code review: commented on pr", "repo", payload.Repository.FullName, "pr", prNum)
			}
		}
	}
}

func (s *Service) reviewAlreadyStored(ctx context.Context, fullName string, prNum int, headSHA string) bool {
	if s.Store == nil || strings.TrimSpace(headSHA) == "" {
		return false
	}
	repo, err := s.Store.Repositories().GetByFullName(ctx, fullName)
	if err != nil {
		return false
	}
	item, err := s.Store.WorkItems().GetByRepoNumber(ctx, repo.ID, prNum)
	if err != nil {
		return false
	}
	setting, err := s.Store.Settings().Get(ctx, "ai.pr_review."+item.ID)
	if err != nil {
		return false
	}
	var result ai.CodeReviewResult
	if err := json.Unmarshal(setting.ValueJSON, &result); err != nil {
		return false
	}
	return result.HeadSHA == headSHA
}

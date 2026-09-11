package webhooksvc

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	htmlpkg "html"
	"strconv"
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
		Draft bool `json:"draft"`
		Head  struct {
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
	s.reviewWg.Add(1)
	go func() {
		defer s.reviewWg.Done()
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

	// 5. 若存在高危安全风险或评分过低，联动 Outbox 发送多渠道安全预警
	if len(reviewRes.SecurityRisks) > 0 || reviewRes.Score < 60 {
		s.notifyHighRiskReview(ctx, payload.Repository.FullName, prNum, &item, reviewRes)
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

// TriggerWorkItemReview 手动触发对指定 PR 工作项的 AI 代码审查，同步返回审查结果。
func (s *Service) TriggerWorkItemReview(ctx context.Context, workItemID string) (*ai.CodeReviewResult, error) {
	if s.AI == nil || !s.AI.IsCodeReviewEnabled() {
		return nil, fmt.Errorf("ai code review is not enabled")
	}
	if s.Store == nil {
		return nil, fmt.Errorf("store is required")
	}

	item, err := s.Store.WorkItems().Get(ctx, workItemID)
	if err != nil {
		return nil, fmt.Errorf("get work item: %w", err)
	}
	if item.Kind != store.WorkItemKindPR {
		return nil, fmt.Errorf("work item %s is not a pull request", workItemID)
	}
	if item.Number <= 0 {
		return nil, fmt.Errorf("invalid pull request number: %d", item.Number)
	}

	timeout := 2 * time.Minute
	if configured := s.AI.EffectiveTimeout() + 30*time.Second; configured > timeout {
		timeout = configured
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	repoRec, err := s.Store.Repositories().Get(ctx, item.RepositoryID)
	if err != nil {
		return nil, fmt.Errorf("get repository: %w", err)
	}

	owner, repo := repoRec.Owner, repoRec.Name
	if owner == "" || repo == "" {
		parts := strings.SplitN(repoRec.FullName, "/", 2)
		if len(parts) == 2 {
			owner, repo = parts[0], parts[1]
		}
	}
	if owner == "" || repo == "" {
		return nil, fmt.Errorf("invalid repository full name: %s", repoRec.FullName)
	}

	var token string
	if s.GitHub != nil && repoRec.InstallationID != nil {
		if instID, err := strconv.ParseInt(*repoRec.InstallationID, 10, 64); err == nil && instID > 0 {
			if tok, err := s.GitHub.InstallationToken(ctx, instID); err == nil {
				token = tok
			} else if s.Logger != nil {
				s.Logger.Warn("ai code review manual: resolve installation token failed", "repo", repoRec.FullName, "error", err)
			}
		}
	}

	headSHA := ""
	if s.GitHub != nil {
		if detail, err := s.GitHub.GetPRDetail(ctx, token, owner, repo, item.Number); err == nil {
			headSHA = detail.Head.SHA
		}
	}
	alreadyCommentedForHead := false
	if headSHA != "" {
		if setting, err := s.Store.Settings().Get(ctx, "ai.pr_review."+item.ID); err == nil {
			var previous ai.CodeReviewResult
			if json.Unmarshal(setting.ValueJSON, &previous) == nil {
				alreadyCommentedForHead = previous.HeadSHA == headSHA && previous.CommentedOnPR
			}
		}
	}

	key := fmt.Sprintf("%s#%d#%s", repoRec.FullName, item.Number, headSHA)
	s.reviewMu.Lock()
	if s.reviewInFlight == nil {
		s.reviewInFlight = make(map[string]struct{})
	}
	if _, exists := s.reviewInFlight[key]; exists {
		s.reviewMu.Unlock()
		return nil, fmt.Errorf("ai code review is already in progress for this pr")
	}
	s.reviewInFlight[key] = struct{}{}
	s.reviewMu.Unlock()
	s.reviewWg.Add(1)
	defer func() {
		s.reviewWg.Done()
		s.reviewMu.Lock()
		delete(s.reviewInFlight, key)
		s.reviewMu.Unlock()
	}()

	var diff string
	if s.GitHub != nil {
		d, err := s.GitHub.GetPRDiff(ctx, token, owner, repo, item.Number)
		if err == nil {
			diff = d
		} else if s.Logger != nil {
			s.Logger.Warn("ai code review manual: get pr diff failed", "repo", repoRec.FullName, "pr", item.Number, "error", err)
		}
	}
	if strings.TrimSpace(diff) == "" {
		return nil, fmt.Errorf("pr diff is empty or unavailable")
	}

	reviewRes, err := s.AI.ReviewPR(ctx, repoRec.FullName, item.Title, item.Author, diff)
	if err != nil {
		return nil, fmt.Errorf("ai review pr: %w", err)
	}
	reviewRes.HeadSHA = headSHA

	settingKey := "ai.pr_review." + item.ID
	raw, marshalErr := json.Marshal(reviewRes)
	if marshalErr != nil {
		return nil, fmt.Errorf("marshal review result: %w", marshalErr)
	}
	if _, saveErr := s.Store.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       settingKey,
		ValueJSON: raw,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "manual_trigger",
	}); saveErr != nil {
		return nil, fmt.Errorf("persist review result: %w", saveErr)
	}

	if s.AI.ShouldCommentOnPR() && s.GitHub != nil && token != "" && !alreadyCommentedForHead {
		commentMD := ai.FormatPRComment(reviewRes)
		if err := s.GitHub.CreateIssueComment(ctx, token, owner, repo, item.Number, commentMD); err == nil {
			reviewRes.CommentedOnPR = true
			if rawUpdated, err := json.Marshal(reviewRes); err == nil {
				_, _ = s.Store.Settings().Upsert(ctx, store.SystemSetting{
					ID:        ulid.Make().String(),
					Key:       settingKey,
					ValueJSON: rawUpdated,
					UpdatedAt: time.Now().UTC(),
					UpdatedBy: "manual_trigger",
				})
			}
		}
	}

	if len(reviewRes.SecurityRisks) > 0 || reviewRes.Score < 60 {
		s.notifyHighRiskReview(ctx, repoRec.FullName, item.Number, &item, reviewRes)
	}

	return reviewRes, nil
}

func (s *Service) notifyHighRiskReview(ctx context.Context, repoFullName string, prNum int, item *store.WorkItem, res *ai.CodeReviewResult) {
	if s.Store == nil || res == nil || item == nil {
		return
	}
	channels, err := s.Store.Channels().List(ctx)
	if err != nil || len(channels) == 0 {
		return
	}

	var hasSub bool
	for _, ch := range channels {
		if ch.Enabled && ch.AcceptsKind(store.WorkItemKindPR) {
			hasSub = true
			break
		}
	}
	if !hasSub {
		return
	}

	title := fmt.Sprintf("🛡️ [代码审查告警] %s PR #%d 发现潜在风险 (评分: %d)", repoFullName, prNum, res.Score)
	var sb strings.Builder
	sb.WriteString("⚠️ <b>PR 代码审查风险预警</b>\n")
	sb.WriteString(fmt.Sprintf("仓库：%s\n", htmlpkg.EscapeString(repoFullName)))
	sb.WriteString(fmt.Sprintf("PR：#%d %s\n", prNum, htmlpkg.EscapeString(item.Title)))
	sb.WriteString(fmt.Sprintf("综合评分：<b>%d / 100</b>\n", res.Score))

	if len(res.SecurityRisks) > 0 {
		sb.WriteString("\n🚨 <b>安全风险：</b>\n")
		for _, r := range res.SecurityRisks {
			sb.WriteString("• " + htmlpkg.EscapeString(r) + "\n")
		}
	}
	if len(res.BreakingRisks) > 0 {
		sb.WriteString("\n💥 <b>破坏性变更：</b>\n")
		for _, b := range res.BreakingRisks {
			sb.WriteString("• " + htmlpkg.EscapeString(b) + "\n")
		}
	}
	if res.Summary != "" {
		sb.WriteString("\n📝 <b>审查结论：</b>\n" + htmlpkg.EscapeString(res.Summary) + "\n")
	}

	body := sb.String()
	htmlURL := item.HTMLURL
	if htmlURL == "" {
		htmlURL = fmt.Sprintf("https://github.com/%s/pull/%d", repoFullName, prNum)
	}

	shaKey := res.HeadSHA
	if shaKey == "" {
		shaKey = item.StateHash
	}

	now := time.Now().UTC()
	for _, ch := range channels {
		if !ch.Enabled || !ch.AcceptsKind(store.WorkItemKindPR) {
			continue
		}
		idem := fmt.Sprintf("%s:ai_review:%s:%s", ch.ID, item.ID, shaKey)
		if _, err := s.Store.Outbox().Create(ctx, store.NotificationOutbox{
			ID:             ulid.Make().String(),
			ChannelID:      ch.ID,
			IdempotencyKey: idem,
			Status:         store.OutboxPending,
			NextAttemptAt:  now,
			Title:          title,
			BodyText:       body,
			HTMLURL:        htmlURL,
			BodyJSON: map[string]any{
				"work_item_id": item.ID,
				"kind":         store.WorkItemKindPR,
				"pr_number":    prNum,
				"score":        res.Score,
				"head_sha":     res.HeadSHA,
			},
			ParseMode: "HTML",
		}); err != nil && !errors.Is(err, store.ErrConflict) && s.Logger != nil {
			s.Logger.Warn("ai code review: create outbox alert failed", "repo", repoFullName, "pr", prNum, "channel", ch.ID, "error", err)
		}
	}
}

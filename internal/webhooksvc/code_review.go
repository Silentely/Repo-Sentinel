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

// 手动触发接口按错误类别映射 HTTP 状态码（见 httpapi.handleTriggerWorkItemAIReview）：
// 能力未启用/依赖缺失 → 503，在途冲突 → 409，目标不可审查 → 400（信息可回传），
// 资源不存在 → 404；其余内部错误经 writeMappedError 脱敏映射，不回传原始错误。
var (
	// ErrReviewNotEnabled 表示 AI 代码审查能力未启用。
	ErrReviewNotEnabled = errors.New("ai code review is not enabled")
	// ErrReviewInProgress 表示同一 PR 的审查任务已在途。
	ErrReviewInProgress = errors.New("ai code review is already in progress for this pr")
	// ErrReviewUnavailable 表示审查依赖未装配或暂时不可用（GitHub 客户端缺失、head SHA 不可得等）。
	ErrReviewUnavailable = errors.New("ai code review dependency unavailable")
	// ErrInvalidReviewTarget 表示目标工作项不是可审查的 PR。
	ErrInvalidReviewTarget = errors.New("work item is not a reviewable pull request")
	// errPRDiffUnavailable 表示 PR Diff 拉取为空（无变更或不可访问）。
	errPRDiffUnavailable = errors.New("pr diff is empty or unavailable")
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
		owner, repo = store.SplitFullName(payload.Repository.FullName)
	}
	prNum := payload.PullRequest.Number
	if prNum == 0 {
		prNum = payload.Number
	}
	if owner == "" || repo == "" || prNum == 0 {
		return
	}
	fullName := payload.Repository.FullName
	if fullName == "" {
		fullName = owner + "/" + repo
	}

	// 审查报告挂在 ai.pr_review.<workItemID> 上；工作项缺失时直接跳过，
	// 避免白耗一次 AI 调用后结果无处落库。
	lookupCtx, cancelLookup := context.WithTimeout(s.baseContext(), 5*time.Second)
	itemID := s.lookupWorkItemID(lookupCtx, fullName, prNum)
	cancelLookup()
	if itemID == "" {
		if s.Logger != nil {
			s.Logger.Debug("ai code review skipped: work item not found", "repo", fullName, "pr", prNum)
		}
		return
	}

	// 同一 PR 同一提交只允许一个审查任务在途，避免 webhook 重试重复消耗 AI 配额。
	key := fmt.Sprintf("%s#%d#%s", fullName, prNum, payload.PullRequest.Head.SHA)
	if !s.reviews.acquire(key) {
		return
	}
	var installationID int64
	if payload.Installation != nil {
		installationID = payload.Installation.ID
	}
	s.launchReview(key, prReviewRequest{
		fullName:       fullName,
		owner:          owner,
		repo:           repo,
		prNum:          prNum,
		itemID:         itemID,
		title:          payload.PullRequest.Title,
		author:         payload.PullRequest.User.Login,
		headSHA:        payload.PullRequest.Head.SHA,
		installationID: installationID,
		updatedBy:      "ai_code_review",
		skipStored:     true,
	})
}

// lookupWorkItemID 解析 PR 工作项 ID（审查报告的挂载点）；仓库或工作项缺失时返回空串。
func (s *Service) lookupWorkItemID(ctx context.Context, fullName string, prNum int) string {
	if s.Store == nil {
		return ""
	}
	repoRec, err := s.Store.Repositories().GetByFullName(ctx, fullName)
	if err != nil {
		return ""
	}
	item, err := s.Store.WorkItems().GetByRepoNumber(ctx, repoRec.ID, prNum)
	if err != nil {
		return ""
	}
	return item.ID
}

// baseContext 返回服务生命周期 context；未注入（测试直构）时回退 context.Background()。
func (s *Service) baseContext() context.Context {
	if s.Background != nil {
		return s.Background
	}
	return context.Background()
}

// prReviewRequest 汇集单次 PR 审查管线的完整输入，由 webhook 自动触发与手动触发两条路径构造。
type prReviewRequest struct {
	fullName string
	owner    string
	repo     string
	prNum    int
	itemID   string
	title    string
	author   string
	headSHA  string
	// installationID GitHub App 安装 ID；<=0 表示无安装上下文（令牌为空，仅公开仓可拉 Diff）。
	installationID int64
	// updatedBy 写入 ai.pr_review.<itemID> 设置时的操作来源标识。
	updatedBy string
	// skipStored 为 true 时，同一 head SHA 已有审查结果则跳过本次审查（自动路径幂等语义）；
	// 手动触发为强制刷新，恒为 false。
	skipStored bool
}

// runPRReview 执行「拉取 Diff → AI 审查 → 持久化 → 可选评论回写 → 高危预警」的公共管线，
// 由两条触发路径共用，避免双份实现随修 bug 漂移。返回 nil 结果表示幂等跳过；
// 各步骤失败以包装错误返回，由调用方统一留痕。
func (s *Service) runPRReview(ctx context.Context, req prReviewRequest) (*ai.CodeReviewResult, error) {
	if req.skipStored && s.reviewAlreadyStored(ctx, req.fullName, req.prNum, req.headSHA) {
		return nil, nil
	}
	token := s.resolveInstallationToken(ctx, req.installationID, req.fullName, req.prNum)

	var diff string
	if s.GitHub != nil {
		d, err := s.GitHub.GetPRDiff(ctx, token, req.owner, req.repo, req.prNum)
		if err != nil {
			return nil, fmt.Errorf("get pr diff: %w", err)
		}
		diff = d
	}
	if strings.TrimSpace(diff) == "" {
		return nil, errPRDiffUnavailable
	}

	reviewRes, err := s.AI.ReviewPR(ctx, req.fullName, req.title, req.author, diff)
	if err != nil {
		return nil, fmt.Errorf("ai review pr: %w", err)
	}
	reviewRes.HeadSHA = req.headSHA

	// 先持久化审查结果，评论回写失败时管理台仍可查看报告。
	if err := s.persistPRReview(ctx, req, reviewRes); err != nil {
		return nil, err
	}
	s.commentOnPR(ctx, req, token, reviewRes)

	// 若存在高危安全风险或评分过低，联动 Outbox 发送多渠道安全预警。
	if len(reviewRes.SecurityRisks) > 0 || reviewRes.Score < 60 {
		if item, err := s.Store.WorkItems().Get(ctx, req.itemID); err == nil {
			s.notifyHighRiskReview(ctx, req.fullName, req.prNum, &item, reviewRes)
		}
	}
	return reviewRes, nil
}

// launchReview 在独立 goroutine 中执行审查管线：脱离调用方取消（手动触发的响应返回、
// 停机取消都不应中断在途审查的持久化闭环），panic 兜底，失败统一 Warn 留痕。
func (s *Service) launchReview(key string, req prReviewRequest) {
	go func() {
		defer s.reviews.release(key)
		defer func() {
			if recovered := recover(); recovered != nil && s.Logger != nil {
				s.Logger.Error("ai code review panic recovered", "repo", req.fullName, "pr", req.prNum, "error", recovered)
			}
		}()
		ctx, cancel := s.reviewContext()
		defer cancel()
		if _, err := s.runPRReview(ctx, req); err != nil && s.Logger != nil {
			s.Logger.Warn("ai code review failed", "repo", req.fullName, "pr", req.prNum, "error", err.Error())
		}
	}()
}

// reviewContext 为单次审查建立执行预算：预算取 max(2min, AI 配置超时+处理余量)，
// 并脱离生命周期 context 的取消，保证关闭过程中在途审查可完整落库（由 WaitReviews 排空等待）。
func (s *Service) reviewContext() (context.Context, context.CancelFunc) {
	budget := 2 * time.Minute
	if configured := s.AI.EffectiveTimeout() + webhookProcessMargin; configured > budget {
		budget = configured
	}
	return context.WithTimeout(context.WithoutCancel(s.baseContext()), budget)
}

// resolveInstallationToken 解析 App 安装令牌；失败留 Warn 并返回空串
// （降级为仅公开仓可拉 Diff、不做评论回写），不阻塞审查主流程。
func (s *Service) resolveInstallationToken(ctx context.Context, installationID int64, fullName string, prNum int) string {
	if s.GitHub == nil || installationID <= 0 {
		return ""
	}
	tok, err := s.GitHub.InstallationToken(ctx, installationID)
	if err != nil {
		if s.Logger != nil {
			s.Logger.Warn("ai code review: resolve installation token failed", "repo", fullName, "pr", prNum, "error", err)
		}
		return ""
	}
	return tok
}

// persistPRReview 序列化并写入审查结果（ai.pr_review.<itemID>）。
func (s *Service) persistPRReview(ctx context.Context, req prReviewRequest, res *ai.CodeReviewResult) error {
	raw, err := json.Marshal(res)
	if err != nil {
		return fmt.Errorf("marshal review result: %w", err)
	}
	if _, err := s.Store.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       "ai.pr_review." + req.itemID,
		ValueJSON: raw,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: req.updatedBy,
	}); err != nil {
		return fmt.Errorf("persist review result: %w", err)
	}
	return nil
}

// commentOnPR 可选模式 B：审查结果持久化成功后在 GitHub PR 下发表评论。
// 评论失败与「已评论」状态回写失败均留 Warn（两条触发路径行为一致，排障有迹可循）；
// 同一 head SHA 已评论过则跳过，避免手动重复触发时刷屏。
func (s *Service) commentOnPR(ctx context.Context, req prReviewRequest, token string, res *ai.CodeReviewResult) {
	if !s.AI.ShouldCommentOnPR() || s.GitHub == nil || token == "" {
		return
	}
	if s.reviewCommentedForHead(ctx, req.itemID, req.headSHA) {
		return
	}
	commentMD := ai.FormatPRComment(res)
	if err := s.GitHub.CreateIssueComment(ctx, token, req.owner, req.repo, req.prNum, commentMD); err != nil {
		if s.Logger != nil {
			s.Logger.Warn("ai code review: comment on pr failed", "repo", req.fullName, "pr", req.prNum, "error", err)
		}
		return
	}
	res.CommentedOnPR = true
	if err := s.persistPRReview(ctx, req, res); err != nil && s.Logger != nil {
		s.Logger.Warn("ai code review: persist comment status failed", "repo", req.fullName, "pr", req.prNum, "error", err.Error())
	}
	if s.Logger != nil {
		s.Logger.Info("ai code review: commented on pr", "repo", req.fullName, "pr", req.prNum)
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

// reviewCommentedForHead 判断该 head SHA 的既有审查结果是否已发表过评论，
// 避免手动重复触发同一提交时在 PR 下刷出多条审查评论。
func (s *Service) reviewCommentedForHead(ctx context.Context, itemID, headSHA string) bool {
	if strings.TrimSpace(headSHA) == "" || s.Store == nil {
		return false
	}
	setting, err := s.Store.Settings().Get(ctx, "ai.pr_review."+itemID)
	if err != nil {
		return false
	}
	var previous ai.CodeReviewResult
	if err := json.Unmarshal(setting.ValueJSON, &previous); err != nil {
		return false
	}
	return previous.HeadSHA == headSHA && previous.CommentedOnPR
}

// TriggerWorkItemReview 手动触发指定 PR 工作项的 AI 代码审查，异步入队并返回 head SHA。
// 完整审查管线（Diff 拉取 + LLM 审计 + 持久化 + 评论回写 + 高危预警）在后台执行：
// 同步执行预算（最长 2 分钟）必然超过 HTTP Server 的 WriteTimeout（45s），慢审查会在
// 服务端写响应时被掐断连接，前端误判失败而审查实际已入库。返回的 head SHA 供调用方
// 轮询 GET /work-items/{id}/ai-review（head_sha 与 reviewed_at 均更新即本次审查完成）。
// 校验失败以哨兵错误返回，HTTP 层按类别映射状态码。
func (s *Service) TriggerWorkItemReview(ctx context.Context, workItemID string) (string, error) {
	// 校验顺序：资源存在性（404）→ 目标可审查性（400）→ 能力开关（503）→ 依赖装配（503），
	// 让调用方优先拿到最精确的资源/入参错误。
	if s.Store == nil {
		return "", ErrReviewUnavailable
	}

	item, err := s.Store.WorkItems().Get(ctx, workItemID)
	if err != nil {
		return "", fmt.Errorf("get work item: %w", err)
	}
	if item.Kind != store.WorkItemKindPR || item.Number <= 0 {
		return "", fmt.Errorf("%w: kind=%s number=%d", ErrInvalidReviewTarget, item.Kind, item.Number)
	}
	if s.AI == nil || !s.AI.IsCodeReviewEnabled() {
		return "", ErrReviewNotEnabled
	}
	if s.GitHub == nil {
		return "", ErrReviewUnavailable
	}
	repoRec, err := s.Store.Repositories().Get(ctx, item.RepositoryID)
	if err != nil {
		return "", fmt.Errorf("get repository: %w", err)
	}
	owner, repo := repoRec.Owner, repoRec.Name
	if owner == "" || repo == "" {
		owner, repo = store.SplitFullName(repoRec.FullName)
	}
	if owner == "" || repo == "" {
		return "", fmt.Errorf("%w: invalid repository full name %q", ErrInvalidReviewTarget, repoRec.FullName)
	}
	fullName := repoRec.FullName
	if fullName == "" {
		fullName = owner + "/" + repo
	}

	// head SHA 是在途互斥 key 与前端轮询比对基准：获取失败无法定义本次审查目标，
	// 直接报错由用户重试。预算 10s，远低于 HTTP WriteTimeout。
	installationID := parseInstallationID(repoRec.InstallationID)
	token := s.resolveInstallationToken(ctx, installationID, fullName, item.Number)
	headCtx, cancelHead := context.WithTimeout(ctx, 10*time.Second)
	detail, err := s.GitHub.GetPRDetail(headCtx, token, owner, repo, item.Number)
	cancelHead()
	if err != nil {
		return "", fmt.Errorf("get pr detail: %w", err)
	}
	headSHA := strings.TrimSpace(detail.Head.SHA)
	if headSHA == "" {
		return "", fmt.Errorf("%w: pr head sha unavailable", ErrReviewUnavailable)
	}

	key := fmt.Sprintf("%s#%d#%s", fullName, item.Number, headSHA)
	if !s.reviews.acquire(key) {
		return "", ErrReviewInProgress
	}
	s.launchReview(key, prReviewRequest{
		fullName:       fullName,
		owner:          owner,
		repo:           repo,
		prNum:          item.Number,
		itemID:         item.ID,
		title:          item.Title,
		author:         item.Author,
		headSHA:        headSHA,
		installationID: installationID,
		updatedBy:      "manual_trigger",
	})
	return headSHA, nil
}

// parseInstallationID 将仓库记录中的安装 ID 字符串解析为 int64；缺失或非法时返回 0。
func parseInstallationID(raw *string) int64 {
	if raw == nil {
		return 0
	}
	id, err := strconv.ParseInt(*raw, 10, 64)
	if err != nil || id <= 0 {
		return 0
	}
	return id
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

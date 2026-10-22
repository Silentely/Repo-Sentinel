package rules

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/jev"
	"github.com/Silentely/Repo-Sentinel/internal/normalizer"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// preparedEvent 持有准备完成的通知事件快照与选中的目标渠道。
type preparedEvent struct {
	Result        normalizer.Result
	Channels      []store.NotificationChannel
	Suppressed    bool
	InferredEvent *store.Event
}

func isSecurityEvent(kind string) bool {
	return kind == store.AlertKindSecretScanning ||
		kind == store.AlertKindCodeScanning ||
		kind == store.AlertKindDependabot
}

func cloneEvent(ev *store.Event) *store.Event {
	if ev == nil {
		return nil
	}
	cp := *ev
	if ev.PayloadSummary != nil {
		cp.PayloadSummary = make(map[string]any, len(ev.PayloadSummary))
		for k, v := range ev.PayloadSummary {
			cp.PayloadSummary[k] = v
		}
	} else {
		cp.PayloadSummary = make(map[string]any)
	}
	return &cp
}

// PrepareNotification 执行通知入桶前统一准备：预检、Jev 裁决、抑制事务留痕与路由计算。
func PrepareNotification(
	ctx context.Context,
	res normalizer.Result,
	repoFullName string,
	client jev.Client,
	cfg config.JevConfig,
	st store.Store,
	logger ...*slog.Logger,
) (preparedEvent, error) {
	if st == nil {
		return preparedEvent{Result: res}, errors.New("rules: store is required for notification preparation")
	}

	channels, err := st.Channels().List(ctx)
	if err != nil {
		return preparedEvent{Result: res}, err
	}

	var l *slog.Logger
	if len(logger) > 0 {
		l = logger[0]
	}

	prep, err := prepareNotificationWithChannels(ctx, res, repoFullName, client, cfg, channels, l)
	if err != nil {
		return prep, err
	}

	// 如果判定 active 抑制，执行事务写入 AuditLog 与 OutboxSuppressed
	if prep.Suppressed && st != nil {
		inferredEv := prep.InferredEvent
		if inferredEv == nil {
			inferredEv = res.Event
		}
		// 尝试事务落库
		if err := suppressNotificationInTx(ctx, st, inferredEv, prep.Channels, repoFullName); err != nil {
			// 任一写失败回滚，取消抑制状态，正常走后续投递，绝不静默丢失事件
			prep.Suppressed = false
		}
	}

	return prep, nil
}

// prepareNotificationWithChannels 在给定候选渠道集合上执行纯计算准备（便于单元测试）。
func prepareNotificationWithChannels(
	ctx context.Context,
	res normalizer.Result,
	repoFullName string,
	client jev.Client,
	cfg config.JevConfig,
	channels []store.NotificationChannel,
	logger ...*slog.Logger,
) (preparedEvent, error) {
	var l *slog.Logger
	if len(logger) > 0 {
		l = logger[0]
	}

	ev := res.Event
	if ev == nil || res.SuppressNotify || ev.SuppressNotification {
		return preparedEvent{Result: res, Suppressed: false}, nil
	}
	if !shouldNotifyRealtime(ev) {
		return preparedEvent{Result: res, Suppressed: false}, nil
	}

	branch := ExtractEventBranch(ev)
	// 计算基础静态可匹配渠道（若原本就零匹配，直接返回）
	staticMatches := selectNotificationChannels(channels, ev, ev, repoFullName, branch, false)
	if len(staticMatches) == 0 {
		return preparedEvent{Result: res, Channels: nil, Suppressed: false}, nil
	}

	inferredEv := cloneEvent(ev)
	fallbackPrep := preparedEvent{
		Result:        res,
		Channels:      staticMatches,
		Suppressed:    false,
		InferredEvent: inferredEv,
	}

	// 私有仓库授权检查
	if res.Repository != nil && res.Repository.IsPrivate && !cfg.AllowPrivateState {
		return fallbackPrep, nil
	}

	// 若 Jev 未启用或无可客户端，走静态规则
	if !cfg.Enabled || client == nil {
		return fallbackPrep, nil
	}

	// 准备问题集
	questions := notificationQuestions(cfg.NoiseFilterEnabled, cfg.RoutingEnabled)
	if len(questions) == 0 {
		return fallbackPrep, nil
	}

	// 构建中性状态
	state, err := jev.BuildNotificationState(*ev, repoFullName)
	if err != nil {
		if l != nil {
			l.WarnContext(ctx, "notification jev build state failed, fallback to raw event",
				"repo", repoFullName,
				"error", err,
			)
		}
		return fallbackPrep, nil
	}

	// 发起 Jev 判定
	result, err := client.Judge(ctx, state, questions)
	if err != nil {
		if l != nil {
			l.WarnContext(ctx, "notification jev judge failed, fallback to raw event",
				"repo", repoFullName,
				"error", err,
				"mode", cfg.Mode,
				"model", cfg.Model,
			)
		}
		// 任何网络、超时、熔断或协议错误均降级为静态原事件放行
		return fallbackPrep, nil
	}

	// 1. 降噪与抑制判定
	isSec := isSecurityEvent(ev.Kind)
	// active 抑制须同时满足：非安全事件、可信 bot、issue/PR、is_actionable <= 0.15、noise_type=bot_chatter (confidence >= 0.85)
	activeSuppressEligible := !isSec && cfg.NoiseFilterEnabled && ev.SenderIsBot &&
		(ev.Kind == store.WorkItemKindIssue || ev.Kind == store.WorkItemKindPR)

	isActionableAns := result.Answers["is_actionable"]
	noiseTypeAns := result.Answers["noise_type"]
	isActionableLow := isActionableAns.Noul != nil && *isActionableAns.Noul <= 0.15
	isBotChatter := noiseTypeAns.Choice != nil && *noiseTypeAns.Choice == "bot_chatter" &&
		noiseTypeAns.Confidence != nil && *noiseTypeAns.Confidence >= 0.85

	wouldSuppress := activeSuppressEligible && isActionableLow && isBotChatter
	isSuppressed := false
	if wouldSuppress {
		if cfg.Mode == "active" {
			isSuppressed = true
			jev.GlobalMetrics().RecordSuppressed()
		} else {
			jev.GlobalMetrics().RecordWouldSuppress()
		}
	}

	if l != nil {
		l.InfoContext(ctx, "notification jev judge completed",
			"repo", repoFullName,
			"model", result.Model,
			"mode", cfg.Mode,
			"would_suppress", wouldSuppress,
			"is_suppressed", isSuppressed,
		)
	}

	// 2. 严重度与路由计算
	localTag := localIncidentTag(ev.Kind)
	if ev.Kind == store.WorkItemKindIssue || ev.Kind == store.WorkItemKindPR {
		if tagAns, ok := result.Answers["incident_tag"]; ok && tagAns.Choice != nil && tagAns.Confidence != nil && *tagAns.Confidence >= 0.85 {
			localTag = *tagAns.Choice
		}
	}

	currentSev := ev.Severity
	if sevAns, ok := result.Answers["severity_tier"]; ok && sevAns.Choice != nil && sevAns.Confidence != nil && *sevAns.Confidence >= 0.85 {
		modelSev := *sevAns.Choice
		// 原严重度缺失时补全，或取 max(原值, 模型值)，绝不降低原有严重度
		if strings.TrimSpace(currentSev) == "" || store.SeverityWeight(modelSev) > store.SeverityWeight(currentSev) {
			currentSev = modelSev
		}
	}

	// 记录快照进 PayloadSummary["jev_routing"]
	inferredEv.Severity = currentSev
	inferredEv.PayloadSummary["jev_routing"] = map[string]any{
		"incident_tag":   localTag,
		"model":          result.Model,
		"severity":       currentSev,
		"mode":           cfg.Mode,
		"would_suppress": wouldSuppress,
		"suppressed":     isSuppressed,
	}

	// 计算目标渠道
	// 在 shadow 模式下，Jev 仅作旁路比对与指标记录，绝不改变真实投递渠道！仅在 active 模式且启用智能路由时生效
	routingActive := cfg.RoutingEnabled && cfg.Mode == "active"
	selectedChannels := selectNotificationChannels(channels, ev, inferredEv, repoFullName, branch, routingActive)

	return preparedEvent{
		Result:        res,
		Channels:      selectedChannels,
		Suppressed:    isSuppressed,
		InferredEvent: inferredEv,
	}, nil
}

func suppressNotificationInTx(
	ctx context.Context,
	st store.Store,
	ev *store.Event,
	channels []store.NotificationChannel,
	repoFullName string,
) error {
	if st == nil || ev == nil {
		return nil
	}
	title, body, htmlURL := renderMessage(ev, repoFullName)
	now := time.Now().UTC()

	stateRaw, _ := jev.BuildNotificationState(*ev, repoFullName)
	stateHash := sha256.Sum256(stateRaw)
	stateHashHex := hex.EncodeToString(stateHash[:])

	metaJSON, _ := json.Marshal(map[string]any{
		"event_id":   ev.ID,
		"repo":       repoFullName,
		"plane":      "notification",
		"mode":       "active",
		"reason":     "jev_noise",
		"state_hash": stateHashHex,
	})

	return st.WithTx(ctx, func(tx store.Store) error {
		// 1. 追加审计日志
		audit := store.AuditLog{
			ID:           ulid.Make().String(),
			Action:       "jev.notification.suppressed",
			ActorType:    "system",
			ActorID:      "jev",
			TargetType:   "event",
			TargetID:     ev.ID,
			MetadataJSON: metaJSON,
			CreatedAt:    now,
		}
		if _, err := tx.Audits().Append(ctx, audit); err != nil {
			return err
		}

		// 2. 为每个预选渠道写入 OutboxSuppressed
		for _, ch := range channels {
			suppressedIdem := fmt.Sprintf("suppressed|%s|%s", ev.ID, ch.ID)
			bodyJSON := map[string]any{
				"event_id":          ev.ID,
				"kind":              ev.Kind,
				"action":            ev.Action,
				"repository":        repoFullName,
				"suppressed_reason": "jev_noise",
				"reason":            "jev_noise",
			}
			outbox := store.NotificationOutbox{
				ID:               ulid.Make().String(),
				ChannelID:        ch.ID,
				EventID:          &ev.ID,
				IdempotencyKey:   suppressedIdem,
				Status:           store.OutboxSuppressed,
				SuppressedReason: "jev_noise",
				NextAttemptAt:    now,
				Title:            title,
				BodyText:         body,
				HTMLURL:          htmlURL,
				BodyJSON:         bodyJSON,
				ParseMode:        "HTML",
			}
			if _, err := tx.Outbox().Create(ctx, outbox); err != nil && !errors.Is(err, store.ErrConflict) {
				return err
			}
		}
		return nil
	})
}

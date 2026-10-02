package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/go-chi/chi/v5"
	"github.com/oklog/ulid/v2"
)

func (s *server) handleListChannels(w http.ResponseWriter, r *http.Request) {
	items, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	masked := make([]map[string]any, 0, len(items))
	for _, ch := range items {
		masked = append(masked, map[string]any{
			"id": ch.ID, "channel_type": ch.ChannelType, "name": ch.Name,
			"enabled": ch.Enabled, "target": ch.Target, "allow_private": ch.AllowPrivate,
			"secret_configured": ch.SecretEnvelope != "",
			// 订阅配置：event_kinds 为 nil 表示订阅全部实时类型。
			"event_kinds": ch.EventKinds, "digest_enabled": ch.DigestEnabled,
			"receive_daily_digest": ch.ReceiveDailyDigest,
			"receive_weekly_report": ch.ReceiveWeeklyReport,
			"receive_monthly_report": ch.ReceiveMonthlyReport,
			"quiet_hours_enabled": ch.QuietHoursEnabled,
			"quiet_hours_start": ch.QuietHoursStart,
			"quiet_hours_end": ch.QuietHoursEnd,
			"quiet_hours_tz": ch.QuietHoursTZ,
			"ignore_bots": ch.IgnoreBots,
			"updated_at":  ch.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": masked})
}

// validChannelType 校验渠道类型白名单（telegram / http_webhook）。
func validChannelType(channelType string) bool {
	return store.IsValidChannelType(channelType)
}

func (s *server) handleUpsertChannel(w http.ResponseWriter, r *http.Request) {
	channelType := strings.TrimSpace(chi.URLParam(r, "type"))
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	var body struct {
		Name          string    `json:"name"`
		Enabled       bool      `json:"enabled"`
		Target        string    `json:"target"`
		Secret        string    `json:"secret"`
		AllowPrivate  bool      `json:"allow_private"`
		EventKinds           *[]string `json:"event_kinds"`
		DigestEnabled        *bool     `json:"digest_enabled"`
		ReceiveDailyDigest   *bool     `json:"receive_daily_digest"`
		ReceiveWeeklyReport  *bool     `json:"receive_weekly_report"`
		ReceiveMonthlyReport *bool     `json:"receive_monthly_report"`
		QuietHoursEnabled    *bool     `json:"quiet_hours_enabled"`
		QuietHoursStart      *string   `json:"quiet_hours_start"`
		QuietHoursEnd        *string   `json:"quiet_hours_end"`
		QuietHoursTZ         *string   `json:"quiet_hours_tz"`
		IgnoreBots           *bool     `json:"ignore_bots"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	name := strings.TrimSpace(body.Name)
	target := strings.TrimSpace(body.Target)
	secret := strings.TrimSpace(body.Secret)

	// 订阅类型白名单校验与修剪去重。
	// 显式提供的列表必须落成非 nil 切片：空数组表示「不订阅实时通知」，
	// 不能塌缩成 nil——AcceptsKind 视 nil 为订阅全部，那会让「全部取消勾选」
	// 变成订阅所有类型，事件被推到管理员明确限定为不接收的目标。
	var cleanedKinds []string
	if body.EventKinds != nil {
		cleanedKinds = make([]string, 0, len(*body.EventKinds))
		seen := make(map[string]bool)
		for _, k := range *body.EventKinds {
			trimmed := strings.TrimSpace(k)
			if trimmed == "" {
				continue
			}
			if !store.IsSubscribableKind(trimmed) {
				s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
				return
			}
			if !seen[trimmed] {
				seen[trimmed] = true
				cleanedKinds = append(cleanedKinds, trimmed)
			}
		}
	}
	existing, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	// 真实存储故障不得按"无既有渠道"处理，否则会静默创建重复渠道并丢失原配置。
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		s.writeMappedError(w, r, err)
		return
	}
	ch := store.NotificationChannel{
		ChannelType: channelType, Name: name, Enabled: body.Enabled,
		Target: target, AllowPrivate: body.AllowPrivate,
		DigestEnabled: true, // 新渠道默认接收每日汇总
	}
	if err == nil {
		ch.ID = existing.ID
		ch.SecretEnvelope = existing.SecretEnvelope
		if ch.Name == "" {
			ch.Name = existing.Name
		}
		// 请求未携带订阅配置时保留现值。
		ch.EventKinds = existing.EventKinds
		ch.DigestEnabled = existing.DigestEnabled
		ch.ReceiveDailyDigest = existing.ReceiveDailyDigest
		ch.ReceiveWeeklyReport = existing.ReceiveWeeklyReport
		ch.ReceiveMonthlyReport = existing.ReceiveMonthlyReport
		ch.QuietHoursEnabled = existing.QuietHoursEnabled
		ch.QuietHoursStart = existing.QuietHoursStart
		ch.QuietHoursEnd = existing.QuietHoursEnd
		ch.QuietHoursTZ = existing.QuietHoursTZ
		ch.IgnoreBots = existing.IgnoreBots
		// 目标留空时保留已有 Chat ID / URL，避免「只改订阅」误清空。
		if target == "" {
			ch.Target = existing.Target
		}
	}
	if ch.Name == "" {
		ch.Name = channelType
	}
	if body.EventKinds != nil {
		ch.EventKinds = cleanedKinds
	}
	if body.DigestEnabled != nil {
		ch.DigestEnabled = *body.DigestEnabled
	}
	if body.ReceiveDailyDigest != nil {
		ch.ReceiveDailyDigest = *body.ReceiveDailyDigest
	}
	if body.ReceiveWeeklyReport != nil {
		ch.ReceiveWeeklyReport = *body.ReceiveWeeklyReport
	}
	if body.ReceiveMonthlyReport != nil {
		ch.ReceiveMonthlyReport = *body.ReceiveMonthlyReport
	}
	if body.QuietHoursEnabled != nil {
		ch.QuietHoursEnabled = *body.QuietHoursEnabled
	}
	if body.QuietHoursStart != nil {
		ch.QuietHoursStart = strings.TrimSpace(*body.QuietHoursStart)
	}
	if body.QuietHoursEnd != nil {
		ch.QuietHoursEnd = strings.TrimSpace(*body.QuietHoursEnd)
	}
	if body.QuietHoursTZ != nil {
		ch.QuietHoursTZ = strings.TrimSpace(*body.QuietHoursTZ)
	}
	if body.IgnoreBots != nil {
		ch.IgnoreBots = *body.IgnoreBots
	}
	if secret != "" {
		if s.dependencies.KeyRing == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
			return
		}
		env, err := s.dependencies.KeyRing.Encrypt(r.Context(), []byte(secret), []byte(notify.AAD))
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		ch.SecretEnvelope = env
	}
	// 环境变量引导：若未提供 secret 且 telegram 配置有 token
	if channelType == store.ChannelTelegram && ch.SecretEnvelope == "" {
		if tok := s.dependencies.Config.Notify.Telegram.Token.Reveal(); tok != "" {
			if s.dependencies.KeyRing != nil {
				if env, err := s.dependencies.KeyRing.Encrypt(r.Context(), []byte(tok), []byte(notify.AAD)); err == nil {
					ch.SecretEnvelope = env
				}
			}
		}
		if ch.Target == "" {
			ch.Target = s.dependencies.Config.Notify.Telegram.ChatID
		}
	}
	saved, err := s.dependencies.Store.Channels().Upsert(r.Context(), ch)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if saved.Enabled {
		// 禁用同类型其它渠道：失败会留下多实例并存，低危但留 Debug 便于排查。
		if err := s.dependencies.Store.Channels().DisableOthersOfType(r.Context(), channelType, saved.ID); err != nil && s.dependencies.Logger != nil {
			s.dependencies.Logger.Warn("disable other channels failed",
				"channel_type", channelType, "kept_channel_id", saved.ID, "error_code", "channel_disable_failed", "error", err.Error())
		}
	}
	if session, ok := sessionFromContext(r.Context()); ok && s.dependencies.Store != nil {
		s.appendAudit(r.Context(), store.AuditLog{
			ID:           ulid.Make().String(),
			Action:       "channel.upsert",
			ActorType:    "admin",
			ActorID:      session.AdminID,
			TargetType:   "channel",
			TargetID:     saved.ID,
			MetadataJSON: []byte("{}"),
			IPAddress:    remoteIPFromContext(r.Context()),
			CreatedAt:    time.Now().UTC(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"id": saved.ID, "channel_type": saved.ChannelType, "enabled": saved.Enabled,
		"target": saved.Target, "secret_configured": saved.SecretEnvelope != "",
	})
}

func (s *server) handleRetryOutbox(w http.ResponseWriter, r *http.Request) {
	id := strings.TrimSpace(chi.URLParam(r, "id"))
	if id == "" {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	if err := s.dependencies.Store.Outbox().RetryDead(r.Context(), id, time.Now().UTC()); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "id": id})
}

// handleRetryAllOutboxDead 一键重新排队全部（或指定渠道）失败投递：后端单次 UPDATE 完成，
// 与逐条重试同一字段语义；channel_type → channel_id 解析与列表端点一致。
func (s *server) handleRetryAllOutboxDead(w http.ResponseWriter, r *http.Request) {
	channelType := strings.TrimSpace(r.URL.Query().Get("channel_type"))
	var channelIDs []string
	if channelType != "" {
		channels, err := s.dependencies.Store.Channels().List(r.Context())
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		channelIDs = resolveChannelIDsByType(channels, channelType)
		if len(channelIDs) == 0 {
			writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "retried": 0})
			return
		}
	}
	n, err := s.dependencies.Store.Outbox().RetryAllDead(r.Context(), channelIDs, time.Now().UTC())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "retried": n})
}

func buildTestScenario(scenario string, now time.Time) (title, bodyText, htmlURL string) {
	ts := now.Format("2006-01-02 15:04 UTC")
	switch strings.ToLower(strings.TrimSpace(scenario)) {
	case "security_alert":
		title = "🚨 [Alert] CVE-2026-8812 (Critical): RCE in org/api"
		bodyText = fmt.Sprintf("🚨 <b>安全告警：CVE-2026-8812</b>\n────────────────\n" +
			"<b>仓库:</b> org/api\n<b>严重等级:</b> CRITICAL\n<b>触发时刻:</b> %s\n" +
			"────────────────\n" +
			"🤖 告警分析\n" +
			"检测到远程代码执行高危漏洞，受影响组件为 HTTP 路由分发器。建议立即升级依赖包至 2.4.1+ 并撤销相关凭证。", ts)
		htmlURL = "https://github.com/org/api/security/advisories/GHSA-2026-test"
	case "ci_failure":
		title = "❌ [Actions] Build and Test failed on main #142"
		bodyText = fmt.Sprintf("❌ <b>工作流构建失败</b>\n────────────────\n" +
			"<b>仓库:</b> org/web\n<b>分支:</b> main\n<b>Run ID:</b> #142\n<b>发生时刻:</b> %s\n" +
			"────────────────\n" +
			"🤖 故障诊断\n" +
			"测试套件在 <code>pkg/auth/jwt_test.go:88</code> 断言失败：Token 过期校验逻辑产生漂移。建议排查时钟同步与租约时间。", ts)
		htmlURL = "https://github.com/org/web/actions/runs/142"
	case "periodic_digest":
		title = fmt.Sprintf("📊 每日摘要 %s", now.Format("2006-01-02"))
		bodyText = fmt.Sprintf("📊 <b>每日运维摘要</b>\n────────────────\n" +
			"过去 24 小时监控活动汇总：\n" +
			"• 新建 Issue: 3 条\n• 合并 PR: 5 个\n• 工作流执行: 18 次（1 次失败）\n" +
			"────────────────\n" +
			"🤖 运维总结\n项目整体运行平稳，核心 PR #89 已并入主干，建议跟进已关闭的 2 个高优先级 Bug。\n生成于 %s", ts)
		htmlURL = "https://github.com/org/api"
	default:
		title = "🔔 测试通知"
		bodyText = fmt.Sprintf("🔔 <b>测试通知</b>\n────────────────\n" +
			"来自 RepoSentinel 的测试消息，发送于 %s。\n" +
			"如果您收到了这条消息，说明通知渠道配置正确！", ts)
		htmlURL = ""
	}
	return title, bodyText, htmlURL
}

func (s *server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	channelType := strings.TrimSpace(chi.URLParam(r, "type"))
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var reqBody struct {
		Scenario string `json:"scenario"`
	}
	if r.Body != nil && r.ContentLength > 0 {
		_ = json.NewDecoder(r.Body).Decode(&reqBody)
	}

	now := time.Now().UTC()
	title, bodyText, htmlURL := buildTestScenario(reqBody.Scenario, now)

	_, err = s.dependencies.Store.Outbox().Create(r.Context(), store.NotificationOutbox{
		ID:             ulid.Make().String(),
		ChannelID:      ch.ID,
		IdempotencyKey: "test|" + ulid.Make().String(),
		Status:         store.OutboxPending,
		NextAttemptAt:  now,
		Title:          title,
		BodyText:       bodyText,
		HTMLURL:        htmlURL,
		ParseMode:      "HTML",
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "queued",
		"channel_type": channelType,
		"scenario":     reqBody.Scenario,
	})
}

func (s *server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	channelType := strings.TrimSpace(chi.URLParam(r, "type"))
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if err := s.dependencies.Store.Channels().Delete(r.Context(), ch.ID); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if session, ok := sessionFromContext(r.Context()); ok && s.dependencies.Store != nil {
		s.appendAudit(r.Context(), store.AuditLog{
			ID:           ulid.Make().String(),
			Action:       "channel.delete",
			ActorType:    "admin",
			ActorID:      session.AdminID,
			TargetType:   "channel",
			TargetID:     ch.ID,
			MetadataJSON: []byte("{}"),
			IPAddress:    remoteIPFromContext(r.Context()),
			CreatedAt:    time.Now().UTC(),
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "channel_type": channelType})
}

func (s *server) handleToggleChannel(w http.ResponseWriter, r *http.Request) {
	channelType := strings.TrimSpace(chi.URLParam(r, "type"))
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	var body struct {
		Enabled bool `json:"enabled"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	ch, err := s.dependencies.Store.Channels().GetByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if err := s.dependencies.Store.Channels().ToggleEnabled(r.Context(), ch.ID, body.Enabled); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"status":       "ok",
		"channel_type": channelType,
		"enabled":      body.Enabled,
	})
}

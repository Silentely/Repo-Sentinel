package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/rules"
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
			"diagnostic":        notify.DiagnoseChannel(ch),
			// 订阅配置：event_kinds 为 nil 表示订阅全部实时类型。
			"event_kinds": ch.EventKinds, "incident_tags": ch.IncidentTags, "digest_enabled": ch.DigestEnabled,
			"receive_daily_digest":   ch.ReceiveDailyDigest,
			"receive_weekly_report":  ch.ReceiveWeeklyReport,
			"receive_monthly_report": ch.ReceiveMonthlyReport,
			"quiet_hours_enabled":    ch.QuietHoursEnabled,
			"quiet_hours_start":      ch.QuietHoursStart,
			"quiet_hours_end":        ch.QuietHoursEnd,
			"quiet_hours_tz":         ch.QuietHoursTZ,
			"ignore_bots":            ch.IgnoreBots,
			"repo_pattern":           ch.RepoPattern,
			"branch_filter":          ch.BranchFilter,
			"min_severity":           ch.MinSeverity,
			"updated_at":             ch.UpdatedAt,
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
		Name                 string    `json:"name"`
		Enabled              bool      `json:"enabled"`
		Target               string    `json:"target"`
		Secret               string    `json:"secret"`
		AllowPrivate         bool      `json:"allow_private"`
		EventKinds           *[]string `json:"event_kinds"`
		IncidentTags         *[]string `json:"incident_tags"`
		DigestEnabled        *bool     `json:"digest_enabled"`
		ReceiveDailyDigest   *bool     `json:"receive_daily_digest"`
		ReceiveWeeklyReport  *bool     `json:"receive_weekly_report"`
		ReceiveMonthlyReport *bool     `json:"receive_monthly_report"`
		QuietHoursEnabled    *bool     `json:"quiet_hours_enabled"`
		QuietHoursStart      *string   `json:"quiet_hours_start"`
		QuietHoursEnd        *string   `json:"quiet_hours_end"`
		QuietHoursTZ         *string   `json:"quiet_hours_tz"`
		IgnoreBots           *bool     `json:"ignore_bots"`
		RepoPattern          *string   `json:"repo_pattern"`
		BranchFilter         *string   `json:"branch_filter"`
		MinSeverity          *string   `json:"min_severity"`
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
	var cleanedTags []string
	if body.IncidentTags != nil {
		seenTags := make(map[string]bool)
		for _, t := range *body.IncidentTags {
			trimmed := strings.TrimSpace(t)
			if trimmed == "" {
				continue
			}
			if !store.IsValidIncidentTag(trimmed) {
				s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"field": "incident_tags"})
				return
			}
			if !seenTags[trimmed] {
				seenTags[trimmed] = true
				cleanedTags = append(cleanedTags, trimmed)
			}
		}
		sort.Strings(cleanedTags)
	}

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
		DigestEnabled:        true, // 新渠道默认接收定期报告
		ReceiveDailyDigest:   true,
		ReceiveWeeklyReport:  true,
		ReceiveMonthlyReport: true,
	}
	if err == nil {
		ch.ID = existing.ID
		ch.SecretEnvelope = existing.SecretEnvelope
		if ch.Name == "" {
			ch.Name = existing.Name
		}
		// 请求未携带订阅配置时保留现值。
		ch.EventKinds = existing.EventKinds
		ch.IncidentTags = existing.IncidentTags
		ch.DigestEnabled = existing.DigestEnabled
		ch.ReceiveDailyDigest = existing.ReceiveDailyDigest
		ch.ReceiveWeeklyReport = existing.ReceiveWeeklyReport
		ch.ReceiveMonthlyReport = existing.ReceiveMonthlyReport
		ch.QuietHoursEnabled = existing.QuietHoursEnabled
		ch.QuietHoursStart = existing.QuietHoursStart
		ch.QuietHoursEnd = existing.QuietHoursEnd
		ch.QuietHoursTZ = existing.QuietHoursTZ
		ch.IgnoreBots = existing.IgnoreBots
		ch.RepoPattern = existing.RepoPattern
		ch.BranchFilter = existing.BranchFilter
		ch.MinSeverity = existing.MinSeverity
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
	if body.IncidentTags != nil {
		if len(cleanedTags) == 0 {
			ch.IncidentTags = nil
		} else {
			ch.IncidentTags = cleanedTags
		}
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
	// 免打扰时段：为空时按 ent schema 默认值补齐（22:00/08:00/UTC），避免空串落库后
	// 判定恒为「时段非法」而静默失效；非空则严格校验，把时区拼写错误当场报给用户。
	if ch.QuietHoursEnabled {
		if ch.QuietHoursStart == "" {
			ch.QuietHoursStart = "22:00"
		}
		if ch.QuietHoursEnd == "" {
			ch.QuietHoursEnd = "08:00"
		}
		if ch.QuietHoursTZ == "" {
			ch.QuietHoursTZ = "UTC"
		}
		if err := rules.ValidateQuietHours(ch.QuietHoursStart, ch.QuietHoursEnd, ch.QuietHoursTZ); err != nil {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{"reason": err.Error()})
			return
		}
	}
	if body.IgnoreBots != nil {
		ch.IgnoreBots = *body.IgnoreBots
	}
	if body.RepoPattern != nil {
		ch.RepoPattern = strings.TrimSpace(*body.RepoPattern)
	}
	if body.BranchFilter != nil {
		ch.BranchFilter = strings.TrimSpace(*body.BranchFilter)
	}
	if body.MinSeverity != nil {
		ch.MinSeverity = strings.TrimSpace(*body.MinSeverity)
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
		"incident_tags": saved.IncidentTags,
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
		bodyText = fmt.Sprintf("🚨 <b>安全告警：CVE-2026-8812</b>\n────────────────\n"+
			"<b>仓库:</b> org/api\n<b>严重等级:</b> CRITICAL\n<b>触发时刻:</b> %s\n"+
			"────────────────\n"+
			"🤖 告警分析\n"+
			"检测到远程代码执行高危漏洞，受影响组件为 HTTP 路由分发器。建议立即升级依赖包至 2.4.1+ 并撤销相关凭证。", ts)
		htmlURL = "https://github.com/org/api/security/advisories/GHSA-2026-test"
	case "ci_failure":
		title = "❌ [Actions] Build and Test failed on main #142"
		bodyText = fmt.Sprintf("❌ <b>工作流构建失败</b>\n────────────────\n"+
			"<b>仓库:</b> org/web\n<b>分支:</b> main\n<b>Run ID:</b> #142\n<b>发生时刻:</b> %s\n"+
			"────────────────\n"+
			"🤖 故障诊断\n"+
			"测试套件在 <code>pkg/auth/jwt_test.go:88</code> 断言失败：Token 过期校验逻辑产生漂移。建议排查时钟同步与租约时间。", ts)
		htmlURL = "https://github.com/org/web/actions/runs/142"
	case "periodic_digest":
		title = fmt.Sprintf("📊 每日摘要 %s", now.Format("2006-01-02"))
		bodyText = fmt.Sprintf("<b>📊 每日运维摘要 %s</b>\n────────────────\n"+
			"📈 <b>过去 24 小时 · 共 26 条监控活动</b>\n"+
			"🚨 告警: 1  |  🐛 缺陷: 2  |  🚀 发布: 1  |  📦 依赖/更新: 22\n"+
			"────────────────\n"+
			"• 🚨 <b>安全告警：</b> <code>org/api</code> <b>#89</b> 发现依赖漏洞 <b>HIGH</b>，需尽快升级\n"+
			"• 🐛 <b>问题跟踪：</b> <code>org/web</code> <b>#45</b> [Bug] 登录态偶发失效已修复并关闭\n"+
			"• 🚀 <b>版本发布：</b> <code>org/web</code> <code>v1.2.0</code> 正式发布\n"+
			"<blockquote expandable>\n"+
			"<b>📦 常规依赖更新与次要动态（点击展开）</b>\n"+
			"• 自动化依赖升级：新建 Issue 3 条、合并 PR 5 个、工作流正常执行 18 次\n"+
			"</blockquote>\n"+
			"<i>⏰ 生成时间：%s</i>", now.Format("2006-01-02"), ts)
		htmlURL = "https://github.com/org/api"
	default:
		title = "🔔 渠道连通性与渲染能力验证"
		bodyText = fmt.Sprintf("🔔 <b>渠道连通性验证成功</b>\n────────────────\n"+
			"<b>测试状态：</b> 🟢 正常通畅\n"+
			"<b>验证时刻：</b> %s\n"+
			"<b>富文本支持：</b> <b>加粗</b> · <i>斜体</i> · <code>代码</code>\n"+
			"────────────────\n"+
			"<blockquote expandable>\n"+
			"<b>💡 渲染能力自检说明</b>\n"+
			"若您看到本折叠块与等宽代码，说明通知渠道富文本引擎运行正常！系统告警、聚合卡片与 AI 诊断将获得最佳排版体验。\n"+
			"</blockquote>", ts)
		htmlURL = "https://github.com"
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

func (s *server) handleNotificationMetrics(w http.ResponseWriter, r *http.Request) {
	items, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var (
		enabledCount int
		healthyCount int
		warningCount int
		errorCount   int
	)

	for _, ch := range items {
		if ch.Enabled {
			enabledCount++
		}
		diag := notify.DiagnoseChannel(ch)
		switch diag.Status {
		case notify.ChannelHealthOK:
			healthyCount++
		case notify.ChannelHealthWarning:
			warningCount++
		case notify.ChannelHealthError:
			errorCount++
		}
	}

	runtimeStatus := "operational"
	if errorCount > 0 {
		runtimeStatus = "degraded"
	}

	// 投递耗时分位数：以最近已送达记录的「入队→送达」时长估算排队与重试带来的尾延迟，
	// 采样窗口固定为最近 200 条，避免在大表上做全量统计。
	latencyMetrics := notify.DeliveryLatencyMetrics{}
	if sentItems, _, listErr := s.dependencies.Store.Outbox().List(r.Context(), store.ListFilter{
		Status:  store.OutboxSent,
		Page:    1,
		PerPage: 200,
	}); listErr == nil {
		latencies := make([]time.Duration, 0, len(sentItems))
		for _, item := range sentItems {
			if item.UpdatedAt.After(item.CreatedAt) {
				latencies = append(latencies, item.UpdatedAt.Sub(item.CreatedAt))
			}
		}
		latencyMetrics = notify.CalculateLatencyPercentiles(latencies)
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"channels": map[string]any{
			"total":         len(items),
			"enabled":       enabledCount,
			"healthy":       healthyCount,
			"warning":       warningCount,
			"misconfigured": errorCount,
		},
		"runtime": map[string]any{
			"status": runtimeStatus,
		},
		"delivery_latency": map[string]any{
			"sample_size": latencyMetrics.Count,
			"basis":       "created_to_sent",
			"window":      "last_200_sent",
			"p50_ms":      latencyMetrics.P50.Milliseconds(),
			"p90_ms":      latencyMetrics.P90.Milliseconds(),
			"p95_ms":      latencyMetrics.P95.Milliseconds(),
			"p99_ms":      latencyMetrics.P99.Milliseconds(),
			"max_ms":      latencyMetrics.Max.Milliseconds(),
		},
	})
}

func (s *server) handleTestAllChannels(w http.ResponseWriter, r *http.Request) {
	channels, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	var enabled []store.NotificationChannel
	for _, ch := range channels {
		if ch.Enabled {
			enabled = append(enabled, ch)
		}
	}

	if len(enabled) == 0 {
		s.writeAPIError(w, r, http.StatusBadRequest, "no_enabled_channels", map[string]any{
			"message": "当前未启用任何通知渠道，请先启用至少一个渠道再进行全渠道体检",
		})
		return
	}

	now := time.Now().UTC()
	title, bodyText, htmlURL := buildTestScenario("", now)
	results := make([]map[string]any, 0, len(enabled))

	for _, ch := range enabled {
		_, err := s.dependencies.Store.Outbox().Create(r.Context(), store.NotificationOutbox{
			ID:             ulid.Make().String(),
			ChannelID:      ch.ID,
			IdempotencyKey: "test-all|" + ch.ID + "|" + ulid.Make().String(),
			Status:         store.OutboxPending,
			NextAttemptAt:  now,
			Title:          title,
			BodyText:       bodyText,
			HTMLURL:        htmlURL,
			ParseMode:      "HTML",
		})
		st := "queued"
		if err != nil {
			st = "failed"
		}
		results = append(results, map[string]any{
			"channel_type": ch.ChannelType,
			"channel_id":   ch.ID,
			"status":       st,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":   len(enabled),
		"results": results,
	})
}

func (s *server) handleListDigestHistory(w http.ResponseWriter, r *http.Request) {
	// 摘要通知在标题上没有可靠的类型标记，这里按有界多页扫描后过滤：单页只看 50 条会让
	// 较早的摘要永久不可见；上限 10 页保证查询开销可控，scan_total 如实反映已扫描条数。
	const (
		pageSize  = 50
		maxPages  = 10
		pageFirst = 1
	)
	var (
		results      []map[string]any
		scanTotal    int
		pagesScanned int
	)
	for page := pageFirst; page < pageFirst+maxPages; page++ {
		items, pageResult, err := s.dependencies.Store.Outbox().List(r.Context(), store.ListFilter{
			Page:    page,
			PerPage: pageSize,
		})
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
		scanTotal = pageResult.Total
		pagesScanned++
		for _, item := range items {
			if !isDigestNotificationTitle(item.Title) {
				continue
			}
			aiUsed, _ := item.BodyJSON["ai"].(bool)
			count, _ := item.BodyJSON["count"].(float64)
			results = append(results, map[string]any{
				"id":          item.ID,
				"title":       item.Title,
				"status":      item.Status,
				"channel_id":  item.ChannelID,
				"created_at":  item.CreatedAt,
				"ai_used":     aiUsed,
				"event_count": int(count),
			})
		}
		// 已取完全部记录或命中扫描上限时结束。
		if page*pageSize >= pageResult.Total || len(items) == 0 {
			break
		}
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"total":         len(results),
		"items":         results,
		"scan_total":    scanTotal,
		"pages_scanned": pagesScanned,
		"scan_limit":    maxPages,
	})
}

// isDigestNotificationTitle 判定通知标题是否属于定期摘要/报告，供历史归档查询过滤。
func isDigestNotificationTitle(title string) bool {
	return strings.Contains(title, "摘要") || strings.Contains(title, "报告") || strings.Contains(title, "Digest")
}

func (s *server) handleExportNotificationBackup(w http.ResponseWriter, r *http.Request) {
	channels, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}

	exported := make([]map[string]any, 0, len(channels))
	for _, ch := range channels {
		// target 可能是内嵌凭据的传入 Webhook（Slack/Discord 的路径即密钥），
		// 导出前统一脱敏，避免 read 作用域令牌把渠道凭据整仓拉走。
		maskedTarget := notify.RedactSensitiveSecrets(ch.Target)
		exported = append(exported, map[string]any{
			"channel_type":           ch.ChannelType,
			"name":                   ch.Name,
			"target":                 maskedTarget,
			"target_masked":          maskedTarget != ch.Target,
			"enabled":                ch.Enabled,
			"allow_private":          ch.AllowPrivate,
			"event_kinds":            ch.EventKinds,
			"incident_tags":          ch.IncidentTags,
			"receive_daily_digest":   ch.ReceiveDailyDigest,
			"receive_weekly_report":  ch.ReceiveWeeklyReport,
			"receive_monthly_report": ch.ReceiveMonthlyReport,
			"quiet_hours_enabled":    ch.QuietHoursEnabled,
			"quiet_hours_start":      ch.QuietHoursStart,
			"quiet_hours_end":        ch.QuietHoursEnd,
			"quiet_hours_tz":         ch.QuietHoursTZ,
			"ignore_bots":            ch.IgnoreBots,
			"repo_pattern":           ch.RepoPattern,
			"branch_filter":          ch.BranchFilter,
			"min_severity":           ch.MinSeverity,
		})
	}

	writeJSON(w, http.StatusOK, map[string]any{
		"version":     "1.0",
		"exported_at": time.Now().UTC(),
		"total":       len(exported),
		"channels":    exported,
	})
}

func (s *server) handleWidgetStatus(w http.ResponseWriter, r *http.Request) {
	channels, err := s.dependencies.Store.Channels().List(r.Context())
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	status := "healthy"
	for _, ch := range channels {
		if ch.Enabled && notify.DiagnoseChannel(ch).Status == notify.ChannelHealthError {
			status = "degraded"
			break
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"system":       status,
		"status_code":  200,
		"last_updated": time.Now().UTC().Format(time.RFC3339),
	})
}

func (s *server) handleNotificationSLA(w http.ResponseWriter, r *http.Request) {
	sent, err := s.dependencies.Store.Outbox().CountByStatus(r.Context(), store.OutboxSent)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	dead, err := s.dependencies.Store.Outbox().CountByStatus(r.Context(), store.OutboxDead)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	// 在途数量单独返回：成功率只按已完成样本计算，在途量大时样本口径必须让运维看得见，
	// 否则大批量投递过程中会被误读为「已达 SLA」。
	inFlight := 0
	for _, status := range []string{store.OutboxPending, store.OutboxSending} {
		count, countErr := s.dependencies.Store.Outbox().CountByStatus(r.Context(), status)
		if countErr != nil {
			s.writeMappedError(w, r, countErr)
			return
		}
		inFlight += count
	}
	completed := sent + dead
	status := "unknown"
	rate := "N/A"
	if completed > 0 {
		rateValue := float64(sent) / float64(completed) * 100
		rate = fmt.Sprintf("%.1f%%", rateValue)
		if rateValue >= 99.0 {
			status = "meeting_target"
		} else {
			status = "below_target"
		}
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"calculated_at":        time.Now().UTC().Format(time.RFC3339),
		"overall_success_rate": rate,
		"sla_status":           status,
		"target_sla":           "99.0%",
		"sent":                 sent,
		"dead":                 dead,
		"in_flight":            inFlight,
		"sample_size":          completed,
		"sample_basis":         "completed_only",
	})
}

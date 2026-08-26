package httpapi

import (
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
			"updated_at": ch.UpdatedAt,
		})
	}
	writeJSON(w, http.StatusOK, map[string]any{"items": masked})
}

// validChannelType 校验渠道类型白名单（telegram / http_webhook）。
func validChannelType(channelType string) bool {
	return channelType == store.ChannelTelegram || channelType == store.ChannelHTTPWebhook
}

func (s *server) handleUpsertChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
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
		EventKinds    *[]string `json:"event_kinds"`
		DigestEnabled *bool     `json:"digest_enabled"`
	}
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	// 订阅类型白名单校验。
	if body.EventKinds != nil {
		for _, k := range *body.EventKinds {
			if !store.IsSubscribableKind(k) {
				s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
				return
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
		ChannelType: channelType, Name: body.Name, Enabled: body.Enabled,
		Target: body.Target, AllowPrivate: body.AllowPrivate,
		DigestEnabled: true, // 新渠道默认接收每日汇总
	}
	if err == nil {
		ch.ID = existing.ID
		ch.SecretEnvelope = existing.SecretEnvelope
		// 请求未携带订阅配置时保留现值。
		ch.EventKinds = existing.EventKinds
		ch.DigestEnabled = existing.DigestEnabled
		// 目标留空时保留已有 Chat ID / URL，避免「只改订阅」误清空。
		if strings.TrimSpace(body.Target) == "" {
			ch.Target = existing.Target
		}
	}
	if body.EventKinds != nil {
		ch.EventKinds = *body.EventKinds
	}
	if body.DigestEnabled != nil {
		ch.DigestEnabled = *body.DigestEnabled
	}
	if body.Secret != "" {
		if s.dependencies.KeyRing == nil {
			s.writeAPIError(w, r, http.StatusServiceUnavailable, errorCodeEncryptionUnavailable, nil)
			return
		}
		env, err := s.dependencies.KeyRing.Encrypt(r.Context(), []byte(body.Secret), []byte(notify.AAD))
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
	writeJSON(w, http.StatusOK, map[string]any{
		"id": saved.ID, "channel_type": saved.ChannelType, "enabled": saved.Enabled,
		"target": saved.Target, "secret_configured": saved.SecretEnvelope != "",
	})
}

func (s *server) handleRetryOutbox(w http.ResponseWriter, r *http.Request) {
	id := chi.URLParam(r, "id")
	if err := s.dependencies.Store.Outbox().RetryDead(r.Context(), id, time.Now().UTC()); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "id": id})
}

func (s *server) handleTestChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	// 幂等键必须唯一：idempotency_key 有 NOT NULL + UNIQUE 约束，
	// 留空会让第二次测试通知撞唯一索引返回 409。
	now := time.Now().UTC()
	_, err = s.dependencies.Store.Outbox().Create(r.Context(), store.NotificationOutbox{
		ID: ulid.Make().String(), ChannelID: ch.ID,
		IdempotencyKey: "test|" + ulid.Make().String(),
		Status:         store.OutboxPending, NextAttemptAt: now,
		Title: "🔔 测试通知",
		// 正文带发送时刻（UTC，与规则通知时间格式一致）：多条测试通知时
		// 用户能确认收到的是哪一条，而不是内容完全相同的重复消息。
		BodyText: fmt.Sprintf(
			"🔔 <b>测试通知</b>\n────────────────\n来自 RepoSentinel 的测试消息，发送于 %s。\n如果您收到了这条消息，说明通知渠道配置正确！",
			now.Format("2006-01-02 15:04 UTC"),
		),
		ParseMode: "HTML",
	})
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "queued", "channel_type": channelType})
}

func (s *server) handleDeleteChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
	if !validChannelType(channelType) {
		s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, nil)
		return
	}
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	if err := s.dependencies.Store.Channels().Delete(r.Context(), ch.ID); err != nil {
		s.writeMappedError(w, r, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "deleted", "channel_type": channelType})
}

func (s *server) handleToggleChannel(w http.ResponseWriter, r *http.Request) {
	channelType := chi.URLParam(r, "type")
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
	ch, err := s.dependencies.Store.Channels().GetEnabledByType(r.Context(), channelType)
	if err != nil {
		// 如果没有已启用的渠道，尝试获取任意一个
		all, listErr := s.dependencies.Store.Channels().List(r.Context())
		if listErr != nil {
			s.writeMappedError(w, r, listErr)
			return
		}
		found := false
		for _, c := range all {
			if c.ChannelType == channelType {
				ch = c
				found = true
				break
			}
		}
		if !found {
			s.writeAPIError(w, r, http.StatusNotFound, errorCodeNotFound, nil)
			return
		}
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

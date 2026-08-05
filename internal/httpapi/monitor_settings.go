package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	defaults := store.DefaultRetentionPolicy()
	out := map[string]any{
		"admin.timezone":                    "UTC",
		"digest.local_time":                 "09:00",
		"digest.send_empty":                 false,
		"notify.aggregate_window_sec":       60,
		"notify.burst_threshold":            15,
		"notify.burst_window_sec":           300,
		"display.closed_limit":              20,
		"retention.events_days":             defaults.EventsDays,
		"retention.outbox_days":             defaults.OutboxDays,
		"retention.webhook_deliveries_days": defaults.WebhookDeliveriesDays,
		"feature.issues":                    true,
		"feature.pull_requests":             true,
		"feature.actions":                   true,
		"feature.security_alerts":           true,
		"report.weekly_enabled":             false,
		"report.weekly_day":                 "monday",
		"report.monthly_enabled":            false,
		"report.monthly_day":                1,
	}
	for key := range out {
		if s, err := s.dependencies.Store.Settings().Get(r.Context(), key); err == nil {
			var v any
			if json.Unmarshal(s.ValueJSON, &v) == nil {
				out[key] = v
			}
		}
	}
	writeJSON(w, http.StatusOK, out)
}

func (s *server) handlePutSettings(w http.ResponseWriter, r *http.Request) {
	var body map[string]any
	if !s.decodeRequestJSON(w, r, &body) {
		return
	}
	allowed := map[string]bool{
		"admin.timezone": true, "digest.local_time": true, "digest.send_empty": true,
		"notify.aggregate_window_sec": true, "notify.burst_threshold": true, "notify.burst_window_sec": true,
		"display.closed_limit":  true,
		"retention.events_days": true, "retention.outbox_days": true, "retention.webhook_deliveries_days": true,
		"feature.issues": true, "feature.pull_requests": true, "feature.actions": true, "feature.security_alerts": true,
		"report.weekly_enabled": true, "report.weekly_day": true,
		"report.monthly_enabled": true, "report.monthly_day": true,
	}
	// 先全量校验再写入：map 遍历顺序随机，边校验边写会导致部分生效——
	// 用户看到 400 却已有键被落库。
	validated := make(map[string]any, len(body))
	for k, v := range body {
		if !allowed[k] {
			continue
		}
		nv, msg, ok := validateSettingValue(k, v)
		if !ok {
			s.writeAPIError(w, r, http.StatusBadRequest, errorCodeValidationFailed, map[string]any{
				"field":   k,
				"message": msg,
			})
			return
		}
		validated[k] = nv
	}
	for k, v := range validated {
		raw, _ := json.Marshal(v)
		_, err := s.dependencies.Store.Settings().Upsert(r.Context(), store.SystemSetting{
			ID: ulid.Make().String(), Key: k, ValueJSON: raw, UpdatedAt: time.Now().UTC(), UpdatedBy: "admin",
		})
		if err != nil {
			s.writeMappedError(w, r, err)
			return
		}
	}
	// 聚合参数热生效：无需重启进程。
	if s.dependencies.Aggregator != nil {
		if err := s.dependencies.Aggregator.ReloadFrom(r.Context()); err != nil {
			s.dependencies.Logger.Warn("aggregator reload failed", "error", err.Error())
		}
	}
	s.handleGetSettings(w, r)
}

// validateSettingValue 校验并规范化单个设置项；返回规范化值、失败提示与是否通过。
func validateSettingValue(key string, v any) (any, string, bool) {
	switch key {
	case "retention.events_days", "retention.outbox_days", "retention.webhook_deliveries_days":
		days, ok := coerceIntInRange(v, 0, 3650)
		return days, "保留天数须为 0–3650 的整数（0 表示禁用该类清理）。", ok
	case "notify.aggregate_window_sec", "notify.burst_window_sec":
		sec, ok := coerceIntInRange(v, 1, 86400)
		return sec, "时间窗须为 1–86400 的整数秒。", ok
	case "notify.burst_threshold":
		n, ok := coerceIntInRange(v, 1, 10000)
		return n, "突发阈值须为 1–10000 的整数。", ok
	case "display.closed_limit":
		n, ok := coerceIntInRange(v, 1, 500)
		return n, "展示上限须为 1–500 的整数。", ok
	case "digest.local_time":
		s, ok := v.(string)
		if !ok {
			return nil, "时间格式须为 HH:MM。", false
		}
		normalized, ok := normalizeLocalTime(s)
		return normalized, "时间格式须为 HH:MM（00:00–23:59）。", ok
	case "admin.timezone":
		s, ok := v.(string)
		if !ok || s == "" {
			return nil, "时区须为 IANA 名称（如 Asia/Shanghai）。", false
		}
		if _, err := time.LoadLocation(s); err != nil {
			return nil, "时区须为 IANA 名称（如 Asia/Shanghai）。", false
		}
		return s, "", true
	case "digest.send_empty",
		"feature.issues", "feature.pull_requests", "feature.actions", "feature.security_alerts",
		"report.weekly_enabled", "report.monthly_enabled":
		b, ok := v.(bool)
		return b, "开关值须为布尔类型。", ok
	case "report.weekly_day":
		s, ok := v.(string)
		if !ok {
			return nil, "发送日须为英文周名（monday–sunday）。", false
		}
		normalized := strings.ToLower(s)
		if !validWeekdayName(normalized) {
			return nil, "发送日须为英文周名（monday–sunday）。", false
		}
		return normalized, "", true
	case "report.monthly_day":
		n, ok := coerceIntInRange(v, 1, 28)
		return n, "每月发送日须为 1–28 的整数。", ok
	default:
		return v, "", true
	}
}

// validWeekdayName 判定是否合法英文周名（小写）。
func validWeekdayName(s string) bool {
	switch s {
	case "sunday", "monday", "tuesday", "wednesday", "thursday", "friday", "saturday":
		return true
	}
	return false
}

// normalizeLocalTime 校验并归一化 HH:MM（允许 9:00，统一输出 09:00）。
func normalizeLocalTime(s string) (string, bool) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", false
	}
	hh, err1 := strconv.Atoi(parts[0])
	mm, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || hh < 0 || hh > 23 || mm < 0 || mm > 59 {
		return "", false
	}
	return fmt.Sprintf("%02d:%02d", hh, mm), true
}

// coerceIntInRange 将 JSON 数值收敛为 [min, max] 内的整数。
func coerceIntInRange(v any, min, max int) (int, bool) {
	n, ok := store.CoerceInt(v)
	if !ok || n < min || n > max {
		return 0, false
	}
	return n, true
}

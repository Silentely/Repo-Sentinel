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

// settingSpec 描述一个可配置的系统设置：默认值、写入白名单与校验规则。
// 设置键的唯一事实来源：GET 默认值、PUT 白名单、校验规则均由此派生，
// 新增设置只改一处，避免三处漂移（历史 bug：PUT 白名单接受 burst_window_sec 而 GET 不返回）。
type settingSpec struct {
	key string
	def any
	// validate 校验并规范化写入值；返回规范化值、失败提示与是否通过。
	validate func(v any) (any, string, bool)
}

// settingSpecs 系统设置注册表（defaults 由注册表统一派生）。
var settingSpecs = func() []settingSpec {
	defaults := store.DefaultRetentionPolicy()
	boolMsg := "开关值须为布尔类型。"
	return []settingSpec{
		{key: "admin.timezone", def: "UTC", validate: validateTimezone},
		{key: "digest.local_time", def: "09:00", validate: validateLocalTime},
		{key: "digest.send_empty", def: false, validate: boolSetting(boolMsg)},
		{key: "notify.aggregate_window_sec", def: 60, validate: intRangeSetting(1, 86400, "时间窗须为 1–86400 的整数秒。")},
		{key: "notify.burst_threshold", def: 15, validate: intRangeSetting(1, 10000, "突发阈值须为 1–10000 的整数。")},
		{key: "notify.burst_window_sec", def: 300, validate: intRangeSetting(1, 86400, "时间窗须为 1–86400 的整数秒。")},
		{key: "display.closed_limit", def: 20, validate: intRangeSetting(1, 500, "展示上限须为 1–500 的整数。")},
		{key: "retention.events_days", def: defaults.EventsDays, validate: intRangeSetting(0, 3650, "保留天数须为 0–3650 的整数（0 表示禁用该类清理）。")},
		{key: "retention.outbox_days", def: defaults.OutboxDays, validate: intRangeSetting(0, 3650, "保留天数须为 0–3650 的整数（0 表示禁用该类清理）。")},
		{key: "retention.webhook_deliveries_days", def: defaults.WebhookDeliveriesDays, validate: intRangeSetting(0, 3650, "保留天数须为 0–3650 的整数（0 表示禁用该类清理）。")},
		{key: "feature.issues", def: true, validate: boolSetting(boolMsg)},
		{key: "feature.pull_requests", def: true, validate: boolSetting(boolMsg)},
		{key: "feature.actions", def: true, validate: boolSetting(boolMsg)},
		{key: "feature.security_alerts", def: true, validate: boolSetting(boolMsg)},
		{key: "report.weekly_enabled", def: false, validate: boolSetting(boolMsg)},
		{key: "report.weekly_day", def: "monday", validate: validateWeekday},
		{key: "report.monthly_enabled", def: false, validate: boolSetting(boolMsg)},
		{key: "report.monthly_day", def: 1, validate: intRangeSetting(1, 28, "每月发送日须为 1–28 的整数。")},
	}
}()

func findSettingSpec(key string) (settingSpec, bool) {
	for _, spec := range settingSpecs {
		if spec.key == key {
			return spec, true
		}
	}
	return settingSpec{}, false
}

func (s *server) handleGetSettings(w http.ResponseWriter, r *http.Request) {
	out := make(map[string]any, len(settingSpecs))
	for _, spec := range settingSpecs {
		out[spec.key] = spec.def
	}
	// 批量读取一次拉齐所有键：逐个 Get 会产生 18 次往返查询，设置页与仪表盘高频调用时无谓放大延迟。
	keys := make([]string, 0, len(out))
	for key := range out {
		keys = append(keys, key)
	}
	if rows, err := s.dependencies.Store.Settings().GetMany(r.Context(), keys...); err == nil {
		for _, row := range rows {
			var v any
			if json.Unmarshal(row.ValueJSON, &v) == nil {
				out[row.Key] = v
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
	// 先全量校验再写入：map 遍历顺序随机，边校验边写会导致部分生效——
	// 用户看到 400 却已有键被落库。白名单即注册表本身，未知键静默忽略。
	validated := make(map[string]any, len(body))
	for k, v := range body {
		spec, ok := findSettingSpec(k)
		if !ok {
			continue
		}
		nv, msg, ok := spec.validate(v)
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
			s.dependencies.Logger.Warn("aggregator reload failed", "error_code", "aggregator_reload_failed", "error", err.Error())
		}
	}
	s.handleGetSettings(w, r)
}

// validateSettingValue 校验并规范化单个设置项；返回规范化值、失败提示与是否通过。
// 保留独立函数签名供既有测试直接调用，实现委托注册表。
func validateSettingValue(key string, v any) (any, string, bool) {
	spec, ok := findSettingSpec(key)
	if !ok {
		return v, "", true
	}
	return spec.validate(v)
}

// boolSetting 生成布尔开关校验器。
func boolSetting(message string) func(any) (any, string, bool) {
	return func(v any) (any, string, bool) {
		b, ok := v.(bool)
		return b, message, ok
	}
}

// intRangeSetting 生成整数区间校验器。
func intRangeSetting(min, max int, message string) func(any) (any, string, bool) {
	return func(v any) (any, string, bool) {
		n, ok := coerceIntInRange(v, min, max)
		return n, message, ok
	}
}

// validateTimezone 校验 IANA 时区名称（自动去除首尾空白）。
func validateTimezone(v any) (any, string, bool) {
	s, ok := v.(string)
	s = strings.TrimSpace(s)
	if !ok || s == "" {
		return nil, "时区须为 IANA 名称（如 Asia/Shanghai）。", false
	}
	if _, err := time.LoadLocation(s); err != nil {
		return nil, "时区须为 IANA 名称（如 Asia/Shanghai）。", false
	}
	return s, "", true
}

// validateLocalTime 校验并归一化 HH:MM（自动去除首尾空白）。
func validateLocalTime(v any) (any, string, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, "时间格式须为 HH:MM。", false
	}
	s = strings.TrimSpace(s)
	normalized, ok := normalizeLocalTime(s)
	return normalized, "时间格式须为 HH:MM（00:00–23:59）。", ok
}

// validateWeekday 校验并归一化英文周名（小写，自动去除首尾空白）。
func validateWeekday(v any) (any, string, bool) {
	s, ok := v.(string)
	if !ok {
		return nil, "发送日须为英文周名（monday–sunday）。", false
	}
	normalized := strings.ToLower(strings.TrimSpace(s))
	if !validWeekdayName(normalized) {
		return nil, "发送日须为英文周名（monday–sunday）。", false
	}
	return normalized, "", true
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

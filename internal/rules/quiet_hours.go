package rules

import (
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// DeliveryAction 描述免打扰决策。
type DeliveryAction string

const (
	ActionDeliverNow      DeliveryAction = "deliver_now"
	ActionDeferQuietHours DeliveryAction = "defer_quiet_hours"
)

// DeliveryDecision 包含免打扰动作、恢复投递时刻及原因。
type DeliveryDecision struct {
	Action   DeliveryAction
	ResumeAt time.Time
	Reason   string
}

// DecideQuietHours 评估通知渠道对于特定事件在当前时刻是否处于免打扰时段。
// 安全高危告警（Secret Scanning、Critical/High 级 Code Scanning 与 Dependabot）拥有穿透豁免权，立即投递。
// 其余事件在免打扰时段内延迟到免打扰结束时刻投递。
func DecideQuietHours(ch store.NotificationChannel, ev *store.Event, now time.Time) (DeliveryAction, time.Time) {
	decision := EvaluateQuietHours(ch, ev, now)
	return decision.Action, decision.ResumeAt
}

// DecideQuietHoursForEvents 评估包含多条事件的聚合通知。
// 聚合消息只要包含一条高危安全告警，就必须继承该告警的静默穿透权，
// 不能只根据聚合列表中的第一条（可能是低危）事件决定投递时机。
func DecideQuietHoursForEvents(ch store.NotificationChannel, events []*store.Event, now time.Time) (DeliveryAction, time.Time) {
	for _, ev := range events {
		if isCriticalSecurityAlert(ev) {
			return ActionDeliverNow, time.Time{}
		}
	}
	if len(events) == 0 {
		return DecideQuietHours(ch, nil, now)
	}
	return DecideQuietHours(ch, events[0], now)
}

// EvaluateQuietHours 返回免打扰完整决策。
func EvaluateQuietHours(ch store.NotificationChannel, ev *store.Event, now time.Time) DeliveryDecision {
	if !ch.QuietHoursEnabled {
		return DeliveryDecision{Action: ActionDeliverNow, Reason: "quiet_hours_disabled"}
	}
	if isCriticalSecurityAlert(ev) {
		return DeliveryDecision{Action: ActionDeliverNow, Reason: "security_alert_bypass"}
	}
	loc := loadLocation(ch.QuietHoursTZ)
	localNow := now.In(loc)
	startMin, okStart := parseTimeOfDay(ch.QuietHoursStart)
	endMin, okEnd := parseTimeOfDay(ch.QuietHoursEnd)
	if !okStart || !okEnd || startMin == endMin {
		return DeliveryDecision{Action: ActionDeliverNow, Reason: "invalid_quiet_hours_range"}
	}

	curMin := localNow.Hour()*60 + localNow.Minute()
	inQuiet := false
	if startMin < endMin {
		inQuiet = curMin >= startMin && curMin < endMin
	} else {
		// 跨午夜区间，如 22:00 - 08:00
		inQuiet = curMin >= startMin || curMin < endMin
	}

	if !inQuiet {
		return DeliveryDecision{Action: ActionDeliverNow, Reason: "outside_quiet_hours"}
	}

	endH := endMin / 60
	endM := endMin % 60
	endToday := time.Date(localNow.Year(), localNow.Month(), localNow.Day(), endH, endM, 0, 0, loc)
	var resumeTime time.Time
	if startMin < endMin {
		resumeTime = endToday
	} else {
		if curMin >= startMin {
			resumeTime = endToday.AddDate(0, 0, 1)
		} else {
			resumeTime = endToday
		}
	}

	return DeliveryDecision{
		Action:   ActionDeferQuietHours,
		ResumeAt: resumeTime.UTC(),
		Reason:   "in_quiet_hours",
	}
}

func isCriticalSecurityAlert(ev *store.Event) bool {
	if ev == nil {
		return false
	}
	switch ev.Kind {
	case store.AlertKindSecretScanning:
		return true
	case store.AlertKindCodeScanning, store.AlertKindDependabot:
		sev := strings.ToLower(strings.TrimSpace(ev.Severity))
		return sev == "critical" || sev == "high"
	default:
		return false
	}
}

func parseTimeOfDay(s string) (int, bool) {
	s = strings.TrimSpace(s)
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, false
	}
	h := 0
	m := 0
	for _, c := range parts[0] {
		if c < '0' || c > '9' {
			return 0, false
		}
		h = h*10 + int(c-'0')
	}
	for _, c := range parts[1] {
		if c < '0' || c > '9' {
			return 0, false
		}
		m = m*10 + int(c-'0')
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, false
	}
	return h*60 + m, true
}

func loadLocation(tz string) *time.Location {
	tz = strings.TrimSpace(tz)
	if tz == "" {
		return time.UTC
	}
	loc, err := time.LoadLocation(tz)
	if err != nil {
		return time.UTC
	}
	return loc
}

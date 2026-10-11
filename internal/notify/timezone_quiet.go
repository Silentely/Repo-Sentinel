package notify

import (
	"fmt"
	"time"
)

// QuietHoursEvaluation 免打扰时间评估结果
type QuietHoursEvaluation struct {
	InQuietHours bool          `json:"in_quiet_hours"`
	LocalTime    string        `json:"local_time"`
	TimeZone     string        `json:"time_zone"`
	Remaining    time.Duration `json:"remaining"` // 距离静默结束剩余时长
}

// EvaluateQuietHoursByTimezone 评估在目标时区下是否处于免打扰区间
// startHour, endHour: 0~23。例如 22 到 8 表示 22:00 到次日 08:00
func EvaluateQuietHoursByTimezone(nowUTC time.Time, tzName string, startHour, endHour int) (QuietHoursEvaluation, error) {
	loc, err := time.LoadLocation(tzName)
	if err != nil {
		return QuietHoursEvaluation{}, fmt.Errorf("invalid timezone %q: %w", tzName, err)
	}

	localTime := nowUTC.In(loc)
	currentHour := localTime.Hour()

	inQuiet := false
	var endTarget time.Time

	if startHour > endHour {
		// 跨午夜区间，例如 22:00 ~ 08:00
		if currentHour >= startHour {
			inQuiet = true
			// 目标结束时间为次日 endHour:00
			nextDay := localTime.Add(24 * time.Hour)
			endTarget = time.Date(nextDay.Year(), nextDay.Month(), nextDay.Day(), endHour, 0, 0, 0, loc)
		} else if currentHour < endHour {
			inQuiet = true
			// 目标结束时间为当日 endHour:00
			endTarget = time.Date(localTime.Year(), localTime.Month(), localTime.Day(), endHour, 0, 0, 0, loc)
		}
	} else if startHour < endHour {
		// 同一天区间，例如 12:00 ~ 14:00
		if currentHour >= startHour && currentHour < endHour {
			inQuiet = true
			endTarget = time.Date(localTime.Year(), localTime.Month(), localTime.Day(), endHour, 0, 0, 0, loc)
		}
	}

	var remaining time.Duration
	if inQuiet {
		remaining = endTarget.Sub(localTime)
		if remaining < 0 {
			remaining = 0
		}
	}

	return QuietHoursEvaluation{
		InQuietHours: inQuiet,
		LocalTime:    localTime.Format("2006-01-02 15:04:05 MST"),
		TimeZone:     tzName,
		Remaining:    remaining,
	}, nil
}

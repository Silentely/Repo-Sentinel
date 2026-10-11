package rules

// RepoHealthReport 仓库健康度评估报告。
type RepoHealthReport struct {
	Score int    `json:"score"`
	Grade string `json:"grade"`
	Level string `json:"level"`
}

// CalculateRepoHealth 根据高危告警、常规告警与 CI 故障数计算仓库健康评分与评级。
func CalculateRepoHealth(highAlerts, normalAlerts, ciFailures int) RepoHealthReport {
	score := 100

	score -= highAlerts * 25
	score -= normalAlerts * 10
	score -= ciFailures * 15

	if score < 0 {
		score = 0
	}

	var grade, level string
	switch {
	case score >= 90:
		grade = "A"
		level = "🟢 优良"
	case score >= 75:
		grade = "B"
		level = "🟡 良好"
	case score >= 60:
		grade = "C"
		level = "🟠 需关注"
	default:
		grade = "D"
		level = "🚨 风险"
	}

	return RepoHealthReport{
		Score: score,
		Grade: grade,
		Level: level,
	}
}

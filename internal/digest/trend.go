package digest

// TrendAnalysis 包含滑动均值与态势描述。
type TrendAnalysis struct {
	Average float64 `json:"average"`
	Trend   string  `json:"trend"`
}

// CalculateMovingAverage 计算过去序列的平均值并评估当前计数的相对偏离态势。
func CalculateMovingAverage(history []int, current int) TrendAnalysis {
	if len(history) == 0 {
		return TrendAnalysis{
			Average: float64(current),
			Trend:   "🌿 处于常态水平",
		}
	}

	sum := 0
	for _, v := range history {
		sum += v
	}
	avg := float64(sum) / float64(len(history))

	var trend string
	if avg > 0 {
		ratio := float64(current) / avg
		switch {
		case ratio >= 1.25:
			trend = "🔥 明显高于近期均值"
		case ratio <= 0.75:
			trend = "💤 明显低于近期均值"
		default:
			trend = "🌿 处于常态水平"
		}
	} else {
		trend = "🌿 处于常态水平"
	}

	return TrendAnalysis{
		Average: avg,
		Trend:   trend,
	}
}

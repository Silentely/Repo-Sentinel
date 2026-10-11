package notify

// WatermarkReport 队列水线背压健康结论。
type WatermarkReport struct {
	Level              string `json:"level"`
	BackpressureActive bool   `json:"backpressure_active"`
	Suggestion         string `json:"suggestion"`
}

// CheckOutboxWatermark 研判发件箱积压深度与系统背压状态。
func CheckOutboxWatermark(pendingCount, deadCount int) WatermarkReport {
	switch {
	case pendingCount >= 500:
		return WatermarkReport{
			Level:              "🚨 严重积压",
			BackpressureActive: true,
			Suggestion:         "当前待发送积压超过 500 条，已自动开启排队节流平滑机制",
		}
	case pendingCount >= 100:
		return WatermarkReport{
			Level:              "⚠️ 轻度积压",
			BackpressureActive: false,
			Suggestion:         "队列处于突发积压，Worker 正在加速消化中",
		}
	default:
		return WatermarkReport{
			Level:              "🟢 健康畅通",
			BackpressureActive: false,
			Suggestion:         "发件队列处理水位正常",
		}
	}
}

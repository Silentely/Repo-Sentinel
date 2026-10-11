package notify

import (
	"sort"
	"time"
)

// DeliveryLatencyMetrics 投递耗时分位数指标
type DeliveryLatencyMetrics struct {
	Count int           `json:"count"`
	Min   time.Duration `json:"min"`
	Max   time.Duration `json:"max"`
	Avg   time.Duration `json:"avg"`
	P50   time.Duration `json:"p50"`
	P90   time.Duration `json:"p90"`
	P95   time.Duration `json:"p95"`
	P99   time.Duration `json:"p99"`
}

// CalculateLatencyPercentiles 从一组投递耗时记录中计算分位数指标
func CalculateLatencyPercentiles(latencies []time.Duration) DeliveryLatencyMetrics {
	n := len(latencies)
	if n == 0 {
		return DeliveryLatencyMetrics{}
	}

	sorted := make([]time.Duration, n)
	copy(sorted, latencies)
	sort.Slice(sorted, func(i, j int) bool {
		return sorted[i] < sorted[j]
	})

	var total time.Duration
	for _, lat := range sorted {
		total += lat
	}

	percentile := func(p float64) time.Duration {
		idx := int(float64(n-1) * p)
		if idx < 0 {
			idx = 0
		}
		if idx >= n {
			idx = n - 1
		}
		return sorted[idx]
	}

	return DeliveryLatencyMetrics{
		Count: n,
		Min:   sorted[0],
		Max:   sorted[n-1],
		Avg:   total / time.Duration(n),
		P50:   percentile(0.50),
		P90:   percentile(0.90),
		P95:   percentile(0.95),
		P99:   percentile(0.99),
	}
}

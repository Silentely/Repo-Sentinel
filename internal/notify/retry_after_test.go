package notify

import (
	"net/http"
	"testing"
	"time"
)

// TestParseRetryAfter 直接验证 parseRetryAfter 的全部分支：
// 整数秒、空白填充、非正数、非法文本、超上限封顶、HTTP 日期及其超限/过期场景。
func TestParseRetryAfter(t *testing.T) {
	future := time.Now().UTC().Add(90 * time.Second)
	farFuture := time.Now().UTC().Add(2 * time.Hour)
	past := time.Now().UTC().Add(-time.Minute)

	cases := []struct {
		name   string
		header string
		want   int // -1 表示按日期容差断言（约 90s）
	}{
		{"空头", "", 0},
		{"整数秒", "120", 120},
		{"空白填充整数", "  120  ", 120},
		{"最小有效秒", "1", 1},
		{"零秒不采纳", "0", 0},
		{"负秒不采纳", "-5", 0},
		{"非整数数字", "1.5", 0},
		{"非法文本", "abc", 0},
		{"混合数字文本", "120abc", 0},
		{"整数超上限封顶", "999999", maxRetryAfter},
		{"未来HTTP日期", future.Format(http.TimeFormat), -1},
		{"空白填充日期", "  " + future.Format(http.TimeFormat) + " ", -1},
		{"日期超上限封顶", farFuture.Format(http.TimeFormat), maxRetryAfter},
		{"已过期日期", past.Format(http.TimeFormat), 0},
		{"非法日期", "not-a-date", 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := &http.Response{Header: http.Header{}}
			if tc.header != "" {
				resp.Header.Set("Retry-After", tc.header)
			}
			got := parseRetryAfter(resp)
			if tc.want == -1 {
				// 90 秒日期按剩余秒数计算，允许少量执行偏差。
				if got < 88 || got > 92 {
					t.Fatalf("期望约 90，实际 %d", got)
				}
				return
			}
			if got != tc.want {
				t.Fatalf("parseRetryAfter() = %d, want %d", got, tc.want)
			}
		})
	}
}

// 日期恰好等于上限时长：按剩余秒数计算不应封顶（尚未超过上限），
// 也不应因执行偏差四舍五入越界（ceil 后仍等于上限）。
func TestParseRetryAfterNilResponseSafe(t *testing.T) {
	if got := parseRetryAfter(nil); got != 0 {
		t.Fatalf("parseRetryAfter(nil) = %d, want 0", got)
	}
	if got := parseRetryAfter(&http.Response{}); got != 0 {
		t.Fatalf("parseRetryAfter(empty resp) = %d, want 0", got)
	}
}

func TestParseRetryAfterDateAtMax(t *testing.T) {
	at := time.Now().UTC().Add(time.Duration(maxRetryAfter) * time.Second)
	resp := &http.Response{Header: http.Header{"Retry-After": []string{at.Format(http.TimeFormat)}}}
	got := parseRetryAfter(resp)
	if got < maxRetryAfter-1 || got > maxRetryAfter {
		t.Fatalf("parseRetryAfter() = %d, want ~%d", got, maxRetryAfter)
	}
}

package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestIsReservedHTTPPathCaseInsensitive 机器路径不得落入 SPA 兜底返回 200 HTML。
// 原实现大小写敏感，/API/v1/dashboard 这类变体会拿到 index.html，
// 与「客户端探测不误判服务正常」的意图不符。
func TestIsReservedHTTPPathCaseInsensitive(t *testing.T) {
	for _, name := range []string{
		"api/v1/dashboard", "API/v1/dashboard", "Api/V1/Dashboard",
		"metrics", "Metrics", "METRICS",
		"health/ready", "Health/Ready",
		"mcp", "MCP", "oauth/token", "OAuth/Token",
		"openapi.json", "OpenAPI.json",
		"auth.md", "AUTH.md",
	} {
		if !isReservedHTTPPath(name) {
			t.Fatalf("%q 应判为保留路径", name)
		}
	}
	// 普通 SPA 路由不受影响。
	for _, name := range []string{"login", "dashboard", "settings", "assets/index-abc.js"} {
		if isReservedHTTPPath(name) {
			t.Fatalf("%q 不应判为保留路径", name)
		}
	}
}

// TestResolveClientIPAllTrustedFallsBackToPeer XFF 全部条目均为受信任地址时，
// 必须回退直连对端而非返回最左段——后者让受信任网段内的主机可自选客户端身份。
func TestResolveClientIPAllTrustedFallsBackToPeer(t *testing.T) {
	trusted := parseTrustedSubnets([]string{"10.0.0.0/8"})
	got := resolveClientIP("10.1.2.3:5000", "10.9.9.9, 10.8.8.8", "", trusted)
	if got != "10.1.2.3" {
		t.Fatalf("全受信任 XFF 应回退直连对端 10.1.2.3，实际 %q", got)
	}

	// 含非受信任条目时仍取第一个非受信任地址（既有行为不变）。
	got = resolveClientIP("10.1.2.3:5000", "203.0.113.7, 10.9.9.9", "", trusted)
	if got != "203.0.113.7" {
		t.Fatalf("应取第一个非受信任地址 203.0.113.7，实际 %q", got)
	}

	// 默认（无受信任代理）下请求头完全不参与。
	got = resolveClientIP("203.0.113.7:5000", "127.0.0.1", "127.0.0.1", nil)
	if got != "203.0.113.7" {
		t.Fatalf("无受信任代理时应忽略请求头，实际 %q", got)
	}
}

// TestResolveClientIPTrustedRealIPFallsBackToPeer 受信任对端给出的 X-Real-IP 若落在
// 受信任网段内，无法与「伪造自选身份」区分，必须与 XFF 同口径回退直连对端。
func TestResolveClientIPTrustedRealIPFallsBackToPeer(t *testing.T) {
	trusted := parseTrustedSubnets([]string{"10.0.0.0/8"})
	if got := resolveClientIP("10.1.2.3:5000", "", "10.9.9.9", trusted); got != "10.1.2.3" {
		t.Fatalf("受信任网段内的 X-Real-IP 应回退直连对端 10.1.2.3，实际 %q", got)
	}
	// 非受信任地址仍按既有语义采信。
	if got := resolveClientIP("10.1.2.3:5000", "", "203.0.113.7", trusted); got != "203.0.113.7" {
		t.Fatalf("非受信任 X-Real-IP 应被采信，实际 %q", got)
	}
}

// TestJoinWebhookURLNormalizesScheme X-Forwarded-Proto 必须归一化：按逗号取首段、
// 大小写归一且只接受 http/https，不得得出 javascript://host/path 这类畸形值。
func TestJoinWebhookURLNormalizesScheme(t *testing.T) {
	cases := []struct {
		name, proto, wantPrefix string
	}{
		{"https", "https", "https://evil.example/webhooks/github"},
		{"HTTPS 大写", "HTTPS", "https://evil.example/webhooks/github"},
		{"多段取首段", "https, http", "https://evil.example/webhooks/github"},
		{"javascript 被拒", "javascript", "http://evil.example/webhooks/github"},
		{"空值回退 http", "", "http://evil.example/webhooks/github"},
	}
	for _, tc := range cases {
		r := httptest.NewRequest(http.MethodGet, "/api/v1/github/config", nil)
		r.Host = "evil.example"
		if tc.proto != "" {
			r.Header.Set("X-Forwarded-Proto", tc.proto)
		}
		if got := joinWebhookURL("", "", r); got != tc.wantPrefix {
			t.Fatalf("%s：joinWebhookURL=%q, want %q", tc.name, got, tc.wantPrefix)
		}
	}

	// 显式配置的 PublicBaseURL 优先，不受请求头影响。
	r := httptest.NewRequest(http.MethodGet, "/api/v1/github/config", nil)
	r.Host = "evil.example"
	r.Header.Set("X-Forwarded-Proto", "javascript")
	if got := joinWebhookURL("https://sentinel.example", "", r); got != "https://sentinel.example/webhooks/github" {
		t.Fatalf("配置优先：%q", got)
	}
}

// TestMCPStarTrendDaysConvergesToEnum MCP get_star_trend 的 days 必须按声明的
// enum 收敛，与 REST /api/v1/stats/star-trend 同一白名单。原实现只做类型转换，
// 调用方传入的任意整数会原样进入 store 的逐日聚合，与已声明的契约不一致。
func TestMCPStarTrendDaysConvergesToEnum(t *testing.T) {
	cases := []struct {
		args map[string]any
		want int
	}{
		{map[string]any{"days": 7}, 7},
		{map[string]any{"days": 30}, 30},
		{map[string]any{"days": 90}, 90},
		{map[string]any{"days": 0}, 0},
		{map[string]any{"days": float64(7)}, 7},
		{map[string]any{"days": 2147483647}, 30},
		{map[string]any{"days": -1}, 30},
		{map[string]any{"days": 1}, 30},
		{map[string]any{"days": "7"}, 30},
		{map[string]any{}, 30},
		{nil, 30},
	}
	for _, tc := range cases {
		if got := mcpStarTrendDays(tc.args); got != tc.want {
			t.Fatalf("mcpStarTrendDays(%v) = %d, want %d", tc.args, got, tc.want)
		}
	}
}

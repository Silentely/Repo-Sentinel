package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestChannelScenariosAndQuietHoursAPI(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 1. Create Slack channel with custom subscriptions and quiet hours
	putResp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/slack",
		`{
			"name": "slack-alerts",
			"enabled": true,
			"target": "https://hooks.slack.com/services/T/B/X",
			"receive_daily_digest": true,
			"receive_weekly_report": false,
			"receive_monthly_report": true,
			"quiet_hours_enabled": true,
			"quiet_hours_start": "23:00",
			"quiet_hours_end": "07:30",
			"quiet_hours_tz": "Asia/Shanghai"
		}`,
		"127.0.0.1:49001", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if putResp.Code != http.StatusOK {
		t.Fatalf("put slack channel failed status=%d body=%s", putResp.Code, putResp.Body.String())
	}

	// Verify channel list reflects all new properties
	listResp := fixture.request(
		t, http.MethodGet, "/api/v1/notifications/channels",
		"", "127.0.0.1:49002", cookies, nil,
	)
	if listResp.Code != http.StatusOK {
		t.Fatalf("list channels failed status=%d body=%s", listResp.Code, listResp.Body.String())
	}
	var listData struct {
		Items []struct {
			ChannelType          string `json:"channel_type"`
			ReceiveDailyDigest   bool   `json:"receive_daily_digest"`
			ReceiveWeeklyReport  bool   `json:"receive_weekly_report"`
			ReceiveMonthlyReport bool   `json:"receive_monthly_report"`
			QuietHoursEnabled    bool   `json:"quiet_hours_enabled"`
			QuietHoursStart      string `json:"quiet_hours_start"`
			QuietHoursEnd        string `json:"quiet_hours_end"`
			QuietHoursTZ         string `json:"quiet_hours_tz"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listResp.Body.Bytes(), &listData); err != nil {
		t.Fatalf("unmarshal list resp: %v", err)
	}
	if len(listData.Items) != 1 || listData.Items[0].ChannelType != store.ChannelSlack {
		t.Fatalf("unexpected list data items: %+v", listData.Items)
	}
	item := listData.Items[0]
	if !item.ReceiveDailyDigest || item.ReceiveWeeklyReport || !item.ReceiveMonthlyReport {
		t.Fatalf("unexpected periodic flags: %+v", item)
	}
	if !item.QuietHoursEnabled || item.QuietHoursStart != "23:00" || item.QuietHoursEnd != "07:30" || item.QuietHoursTZ != "Asia/Shanghai" {
		t.Fatalf("unexpected quiet hours: %+v", item)
	}

	// 2. Trigger test notification with security_alert scenario
	testSecurityResp := fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/slack/test",
		`{"scenario":"security_alert"}`,
		"127.0.0.1:49003", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if testSecurityResp.Code != http.StatusOK {
		t.Fatalf("test security scenario failed: status=%d body=%s", testSecurityResp.Code, testSecurityResp.Body.String())
	}

	// 3. Trigger test notification with ci_failure scenario
	testCIResp := fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/slack/test",
		`{"scenario":"ci_failure"}`,
		"127.0.0.1:49004", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if testCIResp.Code != http.StatusOK {
		t.Fatalf("test CI scenario failed: status=%d body=%s", testCIResp.Code, testCIResp.Body.String())
	}

	// 4. Verify Outbox entries
	outboxItems, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 10})
	if err != nil {
		t.Fatalf("list outbox: %v", err)
	}
	if len(outboxItems) != 2 {
		t.Fatalf("expected 2 outbox items, got %d", len(outboxItems))
	}

	foundSecurity := false
	foundCI := false
	for _, ob := range outboxItems {
		if strings.Contains(ob.Title, "CVE-2026-8812") && strings.Contains(ob.BodyText, "🤖 告警分析") {
			foundSecurity = true
			if ob.HTMLURL == "" {
				t.Errorf("security alert should have HTMLURL")
			}
		}
		if strings.Contains(ob.Title, "Build and Test failed") && strings.Contains(ob.BodyText, "🤖 故障诊断") {
			foundCI = true
		}
	}
	if !foundSecurity {
		t.Errorf("did not find security scenario in outbox")
	}
	if !foundCI {
		t.Errorf("did not find CI scenario in outbox")
	}
}

func TestChannelQuietHoursValidation(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	cases := []struct {
		name string
		body string
	}{
		{
			"起始时间非法",
			`{"name":"qh","enabled":true,"target":"12345","quiet_hours_enabled":true,
				"quiet_hours_start":"25:00","quiet_hours_end":"08:00","quiet_hours_tz":"UTC"}`,
		},
		{
			"起止时间相同",
			`{"name":"qh","enabled":true,"target":"12345","quiet_hours_enabled":true,
				"quiet_hours_start":"08:00","quiet_hours_end":"08:00","quiet_hours_tz":"UTC"}`,
		},
		{
			"时区不存在",
			`{"name":"qh","enabled":true,"target":"12345","quiet_hours_enabled":true,
				"quiet_hours_start":"22:00","quiet_hours_end":"08:00","quiet_hours_tz":"Not/AZone"}`,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			resp := fixture.request(
				t, http.MethodPut, "/api/v1/notifications/channels/telegram",
				tc.body, "127.0.0.1:49201", cookies, headers,
			)
			if resp.Code != http.StatusBadRequest {
				t.Fatalf("非法免打扰配置应返回 400，got status=%d body=%s", resp.Code, resp.Body.String())
			}
		})
	}

	// 非法配置被拒绝后不应留下渠道记录。
	if _, err := fixture.store.Channels().GetEnabledByType(t.Context(), store.ChannelTelegram); err == nil {
		t.Fatal("非法配置不应创建渠道")
	}
}

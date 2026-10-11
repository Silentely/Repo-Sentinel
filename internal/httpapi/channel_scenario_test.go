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

func TestChannelPeriodicReportFlagsViaAPI(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}
	ctx := t.Context()

	// 1. 新建仅带父开关：三个子开关按默认值补齐为 true。
	putResp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-default","enabled":true,"target":"12345","digest_enabled":true}`,
		"127.0.0.1:49101", cookies, headers,
	)
	if putResp.Code != http.StatusOK {
		t.Fatalf("create telegram failed status=%d body=%s", putResp.Code, putResp.Body.String())
	}
	tg, err := fixture.store.Channels().GetEnabledByType(ctx, store.ChannelTelegram)
	if err != nil {
		t.Fatalf("get telegram channel: %v", err)
	}
	if !tg.ReceiveDailyDigest || !tg.ReceiveWeeklyReport || !tg.ReceiveMonthlyReport {
		t.Fatalf("新建渠道应默认接收日/周/月报告: %+v", tg)
	}

	// 2. 新建时显式全关：合法状态，必须原样保存（换渠道类型以走新建分支）。
	offResp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"hook-off","enabled":true,"target":"https://example.com/hook",
			"digest_enabled":true,"receive_daily_digest":false,"receive_weekly_report":false,"receive_monthly_report":false}`,
		"127.0.0.1:49102", cookies, headers,
	)
	if offResp.Code != http.StatusOK {
		t.Fatalf("create webhook failed status=%d body=%s", offResp.Code, offResp.Body.String())
	}
	hook, err := fixture.store.Channels().GetEnabledByType(ctx, store.ChannelHTTPWebhook)
	if err != nil {
		t.Fatalf("get webhook channel: %v", err)
	}
	if hook.ReceiveDailyDigest || hook.ReceiveWeeklyReport || hook.ReceiveMonthlyReport {
		t.Fatalf("新建显式全关被静默改写: daily=%v weekly=%v monthly=%v",
			hook.ReceiveDailyDigest, hook.ReceiveWeeklyReport, hook.ReceiveMonthlyReport)
	}

	// 3. 更新现有渠道为全关：同样必须保持。
	updateResp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-default","enabled":true,"target":"12345",
			"digest_enabled":true,"receive_daily_digest":false,"receive_weekly_report":false,"receive_monthly_report":false}`,
		"127.0.0.1:49103", cookies, headers,
	)
	if updateResp.Code != http.StatusOK {
		t.Fatalf("update telegram failed status=%d body=%s", updateResp.Code, updateResp.Body.String())
	}
	tgAfter, err := fixture.store.Channels().GetEnabledByType(ctx, store.ChannelTelegram)
	if err != nil {
		t.Fatalf("re-get telegram channel: %v", err)
	}
	if tgAfter.ReceiveDailyDigest || tgAfter.ReceiveWeeklyReport || tgAfter.ReceiveMonthlyReport {
		t.Fatalf("更新为全关被静默改写: daily=%v weekly=%v monthly=%v",
			tgAfter.ReceiveDailyDigest, tgAfter.ReceiveWeeklyReport, tgAfter.ReceiveMonthlyReport)
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

func TestDefaultTestNotificationScenario(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// Create a telegram channel
	fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg","enabled":true,"target":"123456"}`,
		"127.0.0.1:49010", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)

	// Trigger default test notification
	resp := fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/telegram/test",
		"", "127.0.0.1:49011", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("test default failed status=%d", resp.Code)
	}

	items, _, err := fixture.store.Outbox().List(t.Context(), store.ListFilter{Page: 1, PerPage: 5})
	if err != nil || len(items) == 0 {
		t.Fatalf("outbox items missing, err=%v", err)
	}
	item := items[0]
	if !strings.Contains(item.Title, "渠道连通性与渲染能力验证") {
		t.Errorf("expected new default title, got %s", item.Title)
	}
	if !strings.Contains(item.BodyText, "<blockquote expandable>") {
		t.Errorf("expected expandable block in test notification, got %s", item.BodyText)
	}
}

func TestNotificationMetricsEndpoint(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// Create a telegram channel
	fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-metrics","enabled":true,"target":"12345678"}`,
		"127.0.0.1:49020", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)

	// Call GET /api/v1/notifications/metrics
	resp := fixture.request(
		t, http.MethodGet, "/api/v1/notifications/metrics",
		"", "127.0.0.1:49021", cookies, nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", resp.Code, resp.Body.String())
	}

	var res struct {
		Channels struct {
			Total         int `json:"total"`
			Enabled       int `json:"enabled"`
			Healthy       int `json:"healthy"`
			Warning       int `json:"warning"`
			Misconfigured int `json:"misconfigured"`
		} `json:"channels"`
		Runtime struct {
			Status string `json:"status"`
		} `json:"runtime"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal metrics response failed: %v", err)
	}

	if res.Channels.Total < 1 || res.Channels.Enabled < 1 {
		t.Errorf("expected at least 1 channel, got %+v", res.Channels)
	}
	if res.Runtime.Status != "operational" {
		t.Errorf("expected operational runtime status, got %s", res.Runtime.Status)
	}
}

func TestBroadcastTestAllChannels(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{CSRFHeaderName: csrf.Value}

	// 1. 无任何启用渠道时测试应报错 400
	badResp := fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/test-all",
		"", "127.0.0.1:49030", cookies, headers,
	)
	if badResp.Code != http.StatusBadRequest {
		t.Fatalf("无渠道时应返回 400, got %d", badResp.Code)
	}

	// 2. 启用一个 telegram 渠道
	fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-all","enabled":true,"target":"12345678"}`,
		"127.0.0.1:49031", cookies, headers,
	)

	// 3. 再次广播体检
	okResp := fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/test-all",
		"", "127.0.0.1:49032", cookies, headers,
	)
	if okResp.Code != http.StatusOK {
		t.Fatalf("广播体检应成功 200, got %d, body: %s", okResp.Code, okResp.Body.String())
	}

	var res struct {
		Total   int `json:"total"`
		Results []struct {
			ChannelType string `json:"channel_type"`
			Status      string `json:"status"`
		} `json:"results"`
	}
	if err := json.Unmarshal(okResp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal broadcast resp failed: %v", err)
	}
	if res.Total != 1 || res.Results[0].Status != "queued" {
		t.Fatalf("expected 1 queued result, got %+v", res)
	}
}

func TestDigestHistoryEndpoint(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 1. Create a telegram channel & trigger a periodic digest test notification
	fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-digest","enabled":true,"target":"12345678"}`,
		"127.0.0.1:49040", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	fixture.request(
		t, http.MethodPost, "/api/v1/notifications/channels/telegram/test",
		`{"scenario":"periodic_digest"}`,
		"127.0.0.1:49041", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)

	// 2. Call GET /api/v1/digest/history
	resp := fixture.request(
		t, http.MethodGet, "/api/v1/digest/history",
		"", "127.0.0.1:49042", cookies, nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", resp.Code, resp.Body.String())
	}

	var res struct {
		Total int `json:"total"`
		Items []struct {
			Title string `json:"title"`
		} `json:"items"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal digest history failed: %v", err)
	}

	if res.Total < 1 || !strings.Contains(res.Items[0].Title, "每日摘要") {
		t.Fatalf("expected at least 1 digest item containing 每日摘要, got %+v", res)
	}
}

func TestNotificationBackupEndpoint(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 1. Create a telegram channel
	fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/telegram",
		`{"name":"tg-backup","enabled":true,"target":"12345678"}`,
		"127.0.0.1:49050", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)

	// 2. Call GET /api/v1/notifications/backup
	resp := fixture.request(
		t, http.MethodGet, "/api/v1/notifications/backup",
		"", "127.0.0.1:49051", cookies, nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", resp.Code, resp.Body.String())
	}

	var res struct {
		Version  string `json:"version"`
		Total    int    `json:"total"`
		Channels []struct {
			Name        string `json:"name"`
			ChannelType string `json:"channel_type"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal backup response failed: %v", err)
	}

	if res.Version != "1.0" || res.Total < 1 {
		t.Fatalf("unexpected backup payload: %+v", res)
	}
	if res.Channels[0].Name != "tg-backup" {
		t.Fatalf("expected channel name tg-backup, got: %s", res.Channels[0].Name)
	}
}

func TestWidgetStatusEndpoint(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	resp := fixture.request(
		t, http.MethodGet, "/api/v1/widget/status",
		"", "127.0.0.1:49060", cookies, nil,
	)
	if resp.Code != http.StatusOK {
		t.Fatalf("expected 200 OK, got %d, body: %s", resp.Code, resp.Body.String())
	}

	var res struct {
		System string `json:"system"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &res); err != nil {
		t.Fatalf("unmarshal widget status failed: %v", err)
	}
	if res.System != "healthy" {
		t.Fatalf("expected healthy, got %s", res.System)
	}
}

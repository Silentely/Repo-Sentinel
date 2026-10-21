package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestChannelHandlersLifecycle(t *testing.T) {
	ring := testHTTPKeyRing(t)
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: ring})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{
		CSRFHeaderName: csrf.Value,
	}

	// 1. 非法渠道类型校验
	badRec := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/unsupported_type", `{}`, "127.0.0.1:45100", cookies, headers)
	assertAPIError(t, badRec, http.StatusBadRequest, errorCodeValidationFailed)

	// 2. Upsert 飞书渠道
	feishuBody := `{"name":"飞书监控群","enabled":true,"target":"https://open.feishu.cn/open-apis/bot/v2/hook/xxx","secret":"my-secret","event_kinds":["issue","pull_request"]}`
	putRec := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/feishu", feishuBody, "127.0.0.1:45101", cookies, headers)
	if putRec.Code != http.StatusOK {
		t.Fatalf("upsert feishu failed: code %d, body: %s", putRec.Code, putRec.Body.String())
	}

	// 3. 测试发送测试通知
	testRec := fixture.request(t, http.MethodPost, "/api/v1/notifications/channels/feishu/test", "", "127.0.0.1:45102", cookies, headers)
	if testRec.Code != http.StatusOK {
		t.Fatalf("test feishu failed: code %d, body: %s", testRec.Code, testRec.Body.String())
	}

	// 4. 禁用渠道（Toggle Enabled = false）
	toggleBody := `{"enabled":false}`
	toggleRec := fixture.request(t, http.MethodPatch, "/api/v1/notifications/channels/feishu/toggle", toggleBody, "127.0.0.1:45103", cookies, headers)
	if toggleRec.Code != http.StatusOK {
		t.Fatalf("toggle feishu disabled failed: code %d, body: %s", toggleRec.Code, toggleRec.Body.String())
	}

	// 5. 渠道在禁用状态下调用 DELETE 删除，确保成功删除（验证 GetByType 优化）
	delRec := fixture.request(t, http.MethodDelete, "/api/v1/notifications/channels/feishu", "", "127.0.0.1:45104", cookies, headers)
	if delRec.Code != http.StatusOK {
		t.Fatalf("delete disabled feishu channel failed: code %d, body: %s", delRec.Code, delRec.Body.String())
	}

	// 验证渠道已被彻底删除
	_, err := fixture.store.Channels().GetByType(context.Background(), store.ChannelFeishu)
	if err != store.ErrNotFound {
		t.Fatalf("expected ErrNotFound after deletion, got %v", err)
	}

	// 验证审计日志记录了 channel.upsert 与 channel.delete
	audits, err := fixture.store.Audits().List(t.Context(), 20, 0)
	if err != nil {
		t.Fatalf("list audits failed: %v", err)
	}
	var hasUpsert, hasDelete bool
	for _, a := range audits {
		if a.Action == "channel.upsert" {
			hasUpsert = true
		}
		if a.Action == "channel.delete" {
			hasDelete = true
		}
	}
	if !hasUpsert || !hasDelete {
		t.Fatalf("expected audit logs for channel.upsert and channel.delete, got: %+v", audits)
	}
}

func TestChannelIgnoreBotsHandler(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{
		CSRFHeaderName: csrf.Value,
	}

	// 1. Upsert with ignore_bots = true
	body := `{"name":"bot-test","enabled":true,"target":"https://open.feishu.cn/open-apis/bot/v2/hook/xxx","ignore_bots":true}`
	putRec := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/feishu", body, "127.0.0.1:45101", cookies, headers)
	if putRec.Code != http.StatusOK {
		t.Fatalf("upsert channel failed: code %d, body: %s", putRec.Code, putRec.Body.String())
	}

	// 2. List channels and verify ignore_bots is true
	listRec := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels", "", "127.0.0.1:45102", cookies, headers)
	if listRec.Code != http.StatusOK {
		t.Fatalf("list channels failed: code %d", listRec.Code)
	}
	var resp struct {
		Items []struct {
			ChannelType string `json:"channel_type"`
			IgnoreBots  bool   `json:"ignore_bots"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRec.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || !resp.Items[0].IgnoreBots {
		t.Fatalf("expected ignore_bots=true, got %+v", resp.Items)
	}

	// 3. Upsert with ignore_bots = false
	bodyFalse := `{"name":"bot-test","enabled":true,"target":"https://open.feishu.cn/open-apis/bot/v2/hook/xxx","ignore_bots":false}`
	putRec2 := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/feishu", bodyFalse, "127.0.0.1:45103", cookies, headers)
	if putRec2.Code != http.StatusOK {
		t.Fatalf("upsert channel failed: code %d", putRec2.Code)
	}

	listRec2 := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels", "", "127.0.0.1:45104", cookies, headers)
	if err := json.Unmarshal(listRec2.Body.Bytes(), &resp); err != nil {
		t.Fatal(err)
	}
	if len(resp.Items) != 1 || resp.Items[0].IgnoreBots {
		t.Fatalf("expected ignore_bots=false, got %+v", resp.Items)
	}
}

func TestAppendAuditRedactsSecrets(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	s := &server{dependencies: Dependencies{Store: fixture.store}}

	token := "ghp_111122223333444455556666777788889999"
	s.appendAudit(t.Context(), store.AuditLog{
		ID:           "audit-test",
		Action:       "test.redact",
		ActorType:    "admin",
		ActorID:      "admin-1",
		TargetType:   "channel",
		TargetID:     "ch-1",
		MetadataJSON: []byte(`{"token":"` + token + `"}`),
		CreatedAt:    time.Now().UTC(),
	})

	audits, err := fixture.store.Audits().List(t.Context(), 10, 0)
	if err != nil || len(audits) == 0 {
		t.Fatalf("expected audit log, got err: %v", err)
	}
	rawMeta := string(audits[0].MetadataJSON)
	if strings.Contains(rawMeta, token) {
		t.Fatalf("审计日志中不应包含真实 token 原文: %s", rawMeta)
	}
	if !strings.Contains(rawMeta, "ghp_***") {
		t.Fatalf("审计日志应包含脱敏标记: %s", rawMeta)
	}
}

func TestChannelIncidentTags(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)
	headers := map[string]string{
		CSRFHeaderName: csrf.Value,
	}

	// 1. 非法标签被拒绝
	invalidBody := `{"name":"invalid-tag","enabled":true,"target":"123","incident_tags":["unknown_tag"]}`
	badRes := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram", invalidBody, "127.0.0.1:45120", cookies, headers)
	assertAPIError(t, badRes, http.StatusBadRequest, errorCodeValidationFailed)

	// 2. 有效标签：支持去重与自动排序
	validBody := `{"name":"ops","enabled":true,"target":"123","incident_tags":["security","ci","security","dependency"]}`
	putRes := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram", validBody, "127.0.0.1:45121", cookies, headers)
	if putRes.Code != http.StatusOK {
		t.Fatalf("upsert channel with incident tags failed: %d, body: %s", putRes.Code, putRes.Body.String())
	}
	var putView struct {
		IncidentTags []string `json:"incident_tags"`
	}
	if err := json.Unmarshal(putRes.Body.Bytes(), &putView); err != nil {
		t.Fatal(err)
	}
	expected := []string{"ci", "dependency", "security"}
	if len(putView.IncidentTags) != len(expected) {
		t.Fatalf("expected tags %v, got %v", expected, putView.IncidentTags)
	}
	for i, tag := range expected {
		if putView.IncidentTags[i] != tag {
			t.Fatalf("tag at index %d expected %s, got %s", i, tag, putView.IncidentTags[i])
		}
	}

	// 3. 渠道列表与备份导出均包含 incident_tags
	listRes := fixture.request(t, http.MethodGet, "/api/v1/notifications/channels", "", "127.0.0.1:45122", cookies, headers)
	if listRes.Code != http.StatusOK {
		t.Fatalf("list channels failed: %d", listRes.Code)
	}
	var listResp struct {
		Items []struct {
			IncidentTags []string `json:"incident_tags"`
		} `json:"items"`
	}
	if err := json.Unmarshal(listRes.Body.Bytes(), &listResp); err != nil {
		t.Fatal(err)
	}
	if len(listResp.Items) != 1 || len(listResp.Items[0].IncidentTags) != 3 {
		t.Fatalf("unexpected list items: %+v", listResp.Items)
	}

	backupRes := fixture.request(t, http.MethodGet, "/api/v1/notifications/backup", "", "127.0.0.1:45123", cookies, headers)
	if backupRes.Code != http.StatusOK {
		t.Fatalf("export backup failed: %d", backupRes.Code)
	}
	var backupResp struct {
		Channels []struct {
			IncidentTags []string `json:"incident_tags"`
		} `json:"channels"`
	}
	if err := json.Unmarshal(backupRes.Body.Bytes(), &backupResp); err != nil {
		t.Fatal(err)
	}
	if len(backupResp.Channels) != 1 || len(backupResp.Channels[0].IncidentTags) != 3 {
		t.Fatalf("unexpected backup channels: %+v", backupResp.Channels)
	}

	// 4. 更新渠道时省略 incident_tags 保留原有标签
	omitBody := `{"enabled":true,"target":"123","ignore_bots":true}`
	putOmit := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram", omitBody, "127.0.0.1:45124", cookies, headers)
	if putOmit.Code != http.StatusOK {
		t.Fatalf("put omit failed: %d", putOmit.Code)
	}
	if err := json.Unmarshal(putOmit.Body.Bytes(), &putView); err != nil {
		t.Fatal(err)
	}
	if len(putView.IncidentTags) != 3 {
		t.Fatalf("omitted incident_tags should be preserved, got %v", putView.IncidentTags)
	}

	// 5. 显式传入 [] 或 null 清空为全接收 (nil)
	clearBody := `{"enabled":true,"target":"123","incident_tags":[]}`
	putClear := fixture.request(t, http.MethodPut, "/api/v1/notifications/channels/telegram", clearBody, "127.0.0.1:45125", cookies, headers)
	if putClear.Code != http.StatusOK {
		t.Fatalf("put clear failed: %d", putClear.Code)
	}
	if err := json.Unmarshal(putClear.Body.Bytes(), &putView); err != nil {
		t.Fatal(err)
	}
	if putView.IncidentTags != nil {
		t.Fatalf("cleared incident_tags should be nil (all accept), got %v", putView.IncidentTags)
	}
}

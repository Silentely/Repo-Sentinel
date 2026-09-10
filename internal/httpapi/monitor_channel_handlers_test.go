package httpapi

import (
	"context"
	"net/http"
	"testing"

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
}

package httpapi

import (
	"net/http"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestUpsertChannelEmptyEventKindsMeansNoSubscription 回归：显式清空订阅列表必须落为
// 「不订阅实时通知」，不得被改写为「订阅全部」。原实现用 var cleanedKinds []string 累积，
// 输入为空数组时保持 nil，而 NotificationChannel.AcceptsKind 视 nil 为订阅全部——
// 管理员在管理台取消全部勾选（前端发送 event_kinds: []，界面摘要显示「不接收实时通知」）
// 并保存后，该渠道实际收到每一种事件，私有仓库的事件标题/仓库名/告警摘要被推到
// 管理员明确限定为不接收的目标。
func TestUpsertChannelEmptyEventKindsMeansNoSubscription(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 先建一个订阅全部（省略 event_kinds）的渠道。
	created := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"probe","enabled":true,"target":"https://example.invalid/hook"}`,
		"127.0.0.1:48001", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if created.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", created.Code, created.Body.String())
	}

	// 显式清空订阅列表。
	cleared := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"probe","enabled":true,"target":"https://example.invalid/hook","event_kinds":[]}`,
		"127.0.0.1:48002", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if cleared.Code != http.StatusOK {
		t.Fatalf("clear status=%d body=%s", cleared.Code, cleared.Body.String())
	}

	rows, err := fixture.store.Channels().List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应只有 1 条渠道，实际 %d", len(rows))
	}
	ch := rows[0]
	if ch.EventKinds == nil {
		t.Fatal("显式空数组被塌缩为 nil（= 订阅全部）")
	}
	if len(ch.EventKinds) != 0 {
		t.Fatalf("订阅列表应为空，实际 %v", ch.EventKinds)
	}
	for _, kind := range []string{
		store.WorkItemKindIssue, store.WorkItemKindPR, store.WorkflowRunKind,
		store.AlertKindDependabot, store.ReleaseKind, store.StarKind,
	} {
		if ch.AcceptsKind(kind) {
			t.Fatalf("已清空订阅的渠道仍接受 %s", kind)
		}
	}
}

// TestUpsertChannelOmittedEventKindsKeepsExisting 对照：省略 event_kinds 字段时保留现值，
// 与「显式空数组」可区分。
func TestUpsertChannelOmittedEventKindsKeepsExisting(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	if resp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"probe","enabled":true,"target":"https://example.invalid/hook","event_kinds":["issue"]}`,
		"127.0.0.1:48003", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	); resp.Code != http.StatusOK {
		t.Fatalf("create status=%d body=%s", resp.Code, resp.Body.String())
	}
	// 只改名称，省略 event_kinds。
	if resp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"renamed","enabled":true,"target":"https://example.invalid/hook"}`,
		"127.0.0.1:48004", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	); resp.Code != http.StatusOK {
		t.Fatalf("rename status=%d body=%s", resp.Code, resp.Body.String())
	}

	rows, err := fixture.store.Channels().List(t.Context())
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(rows) != 1 {
		t.Fatalf("应只有 1 条渠道，实际 %d", len(rows))
	}
	if len(rows[0].EventKinds) != 1 || rows[0].EventKinds[0] != store.WorkItemKindIssue {
		t.Fatalf("省略字段应保留现值，实际 %v", rows[0].EventKinds)
	}
	if rows[0].Name != "renamed" {
		t.Fatalf("名称未更新: %q", rows[0].Name)
	}
}

// TestUpsertChannelDisabledChannelPreservesIdentityAndSecret 回归：已禁用的渠道在被编辑或重新启用时，
// 必须能够通过 GetByType 识别为既有渠道，保留其主键 ID 与已加密的密钥，避免配置丢失或产生重复记录。
func TestUpsertChannelDisabledChannelPreservesIdentityAndSecret(t *testing.T) {
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: testHTTPKeyRing(t)})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	// 1. 创建并禁用一个包含密钥的渠道
	if resp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"probe-hook","enabled":false,"target":"https://example.invalid/disabled","secret":"secret-token-123"}`,
		"127.0.0.1:48010", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	); resp.Code != http.StatusOK {
		t.Fatalf("create disabled channel status=%d body=%s", resp.Code, resp.Body.String())
	}

	rows, err := fixture.store.Channels().List(t.Context())
	if err != nil || len(rows) != 1 {
		t.Fatalf("expected 1 channel, got %d (err: %v)", len(rows), err)
	}
	initialID := rows[0].ID
	initialEnvelope := rows[0].SecretEnvelope
	if initialEnvelope == "" {
		t.Fatal("expected secret envelope to be populated")
	}

	// 2. 重新编辑该已禁用的渠道（未重复输入 secret，仅修改 target 并启用）
	if resp := fixture.request(
		t, http.MethodPut, "/api/v1/notifications/channels/http_webhook",
		`{"name":"probe-hook-updated","enabled":true,"target":"https://example.invalid/enabled"}`,
		"127.0.0.1:48011", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	); resp.Code != http.StatusOK {
		t.Fatalf("update disabled channel status=%d body=%s", resp.Code, resp.Body.String())
	}

	rowsAfter, err := fixture.store.Channels().List(t.Context())
	if err != nil || len(rowsAfter) != 1 {
		t.Fatalf("expected still exactly 1 channel, got %d (err: %v)", len(rowsAfter), err)
	}
	if rowsAfter[0].ID != initialID {
		t.Fatalf("channel ID should be preserved: expected %s, got %s", initialID, rowsAfter[0].ID)
	}
	if rowsAfter[0].SecretEnvelope != initialEnvelope {
		t.Fatalf("secret envelope should be preserved when omitted: expected %s, got %s", initialEnvelope, rowsAfter[0].SecretEnvelope)
	}
	if rowsAfter[0].Target != "https://example.invalid/enabled" || !rowsAfter[0].Enabled {
		t.Fatalf("target and enabled should be updated, got target=%s enabled=%v", rowsAfter[0].Target, rowsAfter[0].Enabled)
	}
}

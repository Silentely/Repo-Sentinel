package httpapi

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// mcpRequest 以 JSON 请求体调用 /mcp，返回解码后的 JSON-RPC 响应。
func mcpRequest(t *testing.T, fixture *httpTestFixture, token, body string) (int, map[string]any) {
	t.Helper()
	headers := map[string]string{}
	if token != "" {
		headers["Authorization"] = "Bearer " + token
	}
	response := fixture.requestWithContentType(
		t, http.MethodPost, "/mcp", body, "application/json", "127.0.0.1:43001", nil, headers,
	)
	var payload map[string]any
	if response.Body.Len() > 0 {
		if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
			t.Fatalf("MCP 响应不是合法 JSON: %v；响应=%s", err, response.Body.String())
		}
	}
	return response.Code, payload
}

func mcpAccessToken(t *testing.T, fixture *httpTestFixture) string {
	t.Helper()
	payload := requestToken(t, fixture, "grant_type=client_credentials&client_id="+oauthTestClientID+
		"&client_secret="+oauthTestClientSecret)
	token, _ := payload["access_token"].(string)
	if token == "" {
		t.Fatal("未能获取 MCP 测试令牌")
	}
	return token
}

func TestMCPOfflineArgParsers(t *testing.T) {
	if got := mcpIntArg(nil, "page"); got != 0 {
		t.Fatalf("mcpIntArg(nil) = %d, want 0", got)
	}
	if got := mcpStringArg(nil, "key"); got != "" {
		t.Fatalf("mcpStringArg(nil) = %q, want empty", got)
	}

	args := map[string]any{
		"int_val":     int(42),
		"int64_val":   int64(100),
		"float_val":   float64(7),
		"json_num":    json.Number("88"),
		"str_trimmed": "  hello sentinel  ",
	}

	if got := mcpIntArg(args, "int_val"); got != 42 {
		t.Fatalf("int_val = %d, want 42", got)
	}
	if got := mcpIntArg(args, "int64_val"); got != 100 {
		t.Fatalf("int64_val = %d, want 100", got)
	}
	if got := mcpIntArg(args, "float_val"); got != 7 {
		t.Fatalf("float_val = %d, want 7", got)
	}
	if got := mcpIntArg(args, "json_num"); got != 88 {
		t.Fatalf("json_num = %d, want 88", got)
	}
	if got := mcpStringArg(args, "str_trimmed"); got != "hello sentinel" {
		t.Fatalf("str_trimmed = %q, want hello sentinel", got)
	}
}

// TestMCPInitialize返回协议版本与服务器信息 验证 MCP 握手。
func TestMCPInitialize返回协议版本与服务器信息(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	status, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18","capabilities":{},"clientInfo":{"name":"test","version":"1"}}}`)

	if status != http.StatusOK {
		t.Fatalf("状态=%d 响应=%+v", status, payload)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 result: %+v", payload)
	}
	if result["protocolVersion"] != "2025-06-18" {
		t.Fatalf("protocolVersion=%v", result["protocolVersion"])
	}
	serverInfo, ok := result["serverInfo"].(map[string]any)
	if !ok || serverInfo["name"] != "reposentinel" {
		t.Fatalf("serverInfo=%v", result["serverInfo"])
	}
}

// TestMCPToolsList暴露只读工具 验证 tools/list。
func TestMCPToolsList暴露只读工具(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	status, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":2,"method":"tools/list","params":{}}`)

	if status != http.StatusOK {
		t.Fatalf("状态=%d", status)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 result: %+v", payload)
	}
	tools, ok := result["tools"].([]any)
	if !ok || len(tools) == 0 {
		t.Fatalf("tools 为空: %+v", result)
	}
	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names[name] = true
		if tool["description"] == nil || tool["inputSchema"] == nil {
			t.Fatalf("工具 %q 缺少 description/inputSchema: %+v", name, tool)
		}
	}
	for _, want := range []string{"get_dashboard", "list_repositories", "list_work_items", "list_security_alerts"} {
		if !names[want] {
			t.Fatalf("tools/list 缺少 %q: %+v", want, names)
		}
	}
}

// TestMCPToolsCall返回真实数据 验证 tools/call 贯通 Store。
func TestMCPToolsCall返回真实数据(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	now := time.Now().UTC()
	_, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID: "repo-mcp-1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "app", FullName: "acme/app",
	})
	if err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	_, _, err = fixture.store.WorkItems().UpsertIfNewer(t.Context(), store.WorkItem{
		ID: "wi-mcp-1", RepositoryID: "repo-mcp-1", Number: 3, Kind: store.WorkItemKindIssue,
		State: "open", Title: "agent visible issue", SourceUpdatedAt: now, StateHash: "h1",
	}, nil)
	if err != nil {
		t.Fatalf("upsert work item: %v", err)
	}

	status, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_repositories","arguments":{"per_page":10}}}`)
	if status != http.StatusOK {
		t.Fatalf("状态=%d 响应=%+v", status, payload)
	}
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 result: %+v", payload)
	}
	if isError, _ := result["isError"].(bool); isError {
		t.Fatalf("工具执行失败: %+v", result)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content 为空: %+v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "acme/app") {
		t.Fatalf("工具结果缺少仓库数据: %s", text)
	}

	// get_dashboard 也应贯通。
	_, payload = mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"get_dashboard","arguments":{}}}`)
	text = mcpResultText(t, payload)
	if !strings.Contains(text, "open_issues") {
		t.Fatalf("dashboard 结果异常: %s", text)
	}
}

// TestMCPListRepositories类型过滤 守护 type 枚举值与存储取值一致：
// github_installation / external_public 是真实存储值（此前 schema 写的 github/external 永远查空）。
func TestMCPListRepositories类型过滤(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	if _, err := fixture.store.Repositories().Upsert(t.Context(), store.Repository{
		ID: "repo-mcp-t1", Type: store.RepositoryTypeInstallation, SyncStatus: store.SyncStatusActive,
		Owner: "acme", Name: "app", FullName: "acme/app",
	}); err != nil {
		t.Fatalf("upsert repo: %v", err)
	}
	_, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"list_repositories","arguments":{"type":"github_installation"}}}`)
	text := mcpResultText(t, payload)
	if !strings.Contains(text, "acme/app") {
		t.Fatalf("type=github_installation 应命中安装仓: %s", text)
	}
	_, payload = mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":7,"method":"tools/call","params":{"name":"list_repositories","arguments":{"type":"external_public"}}}`)
	text = mcpResultText(t, payload)
	if strings.Contains(text, "acme/app") {
		t.Fatalf("type=external_public 不应命中安装仓: %s", text)
	}
}

func mcpResultText(t *testing.T, payload map[string]any) string {
	t.Helper()
	result, ok := payload["result"].(map[string]any)
	if !ok {
		t.Fatalf("缺少 result: %+v", payload)
	}
	content, ok := result["content"].([]any)
	if !ok || len(content) == 0 {
		t.Fatalf("content 为空: %+v", result)
	}
	text, _ := content[0].(map[string]any)["text"].(string)
	return text
}

// TestMCP未知方法与未知工具返回JSONRPC错误 验证错误分支。
func TestMCP未知方法与未知工具返回JSONRPC错误(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)

	_, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":5,"method":"nope","params":{}}`)
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("未知方法未返回 error: %+v", payload)
	}

	_, payload = mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":6,"method":"tools/call","params":{"name":"does_not_exist","arguments":{}}}`)
	if _, ok := payload["error"].(map[string]any); !ok {
		t.Fatalf("未知工具未返回 error: %+v", payload)
	}
}

// TestMCP未认证返回401 未携带凭据时拒绝。
func TestMCP未认证返回401(t *testing.T) {
	fixture := oauthFixture(t)
	response := fixture.requestWithContentType(
		t, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":7,"method":"tools/list","params":{}}`,
		"application/json", "127.0.0.1:43002", nil, nil,
	)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("状态=%d，期望 401；响应=%s", response.Code, response.Body.String())
	}
}

// TestMCP会话Cookie亦可访问 管理台登录态应同样可用。
func TestMCP会话Cookie亦可访问(t *testing.T) {
	fixture := oauthFixture(t)
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)

	response := fixture.requestWithContentType(
		t, http.MethodPost, "/mcp", `{"jsonrpc":"2.0","id":8,"method":"ping","params":{}}`,
		"application/json", "127.0.0.1:43003", cookies, nil,
	)
	if response.Code != http.StatusOK {
		t.Fatalf("状态=%d 响应=%s", response.Code, response.Body.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(response.Body.Bytes(), &payload); err != nil {
		t.Fatalf("ping 响应不是合法 JSON: %v", err)
	}
	if _, ok := payload["result"]; !ok {
		t.Fatalf("ping 缺少 result: %+v", payload)
	}
}

// TestMCPInitialize协议版本与serverInfo 守护：协商版本随响应头回写、
// serverInfo.version 在 dev 构建下回退非空。
func TestMCPInitialize协议版本与serverInfo(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	auth := map[string]string{"Authorization": "Bearer " + token}

	resp := fixture.requestWithContentType(t, http.MethodPost, "/mcp",
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-03-26"}}`,
		"application/json", "127.0.0.1:43002", nil, auth)
	if got := resp.Header().Get("MCP-Protocol-Version"); got != "2025-03-26" {
		t.Fatalf("协商版本应随响应头回写，got %q", got)
	}
	var payload map[string]any
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatal(err)
	}
	result, _ := payload["result"].(map[string]any)
	info, _ := result["serverInfo"].(map[string]any)
	if v, _ := info["version"].(string); v == "" {
		t.Fatalf("serverInfo.version 不应为空: %+v", info)
	}

	// 未指定协议版本：回默认版本头。
	resp2 := fixture.requestWithContentType(t, http.MethodPost, "/mcp",
		`{"jsonrpc":"2.0","id":2,"method":"initialize","params":{}}`,
		"application/json", "127.0.0.1:43003", nil, auth)
	if got := resp2.Header().Get("MCP-Protocol-Version"); got != mcpDefaultProtocolVersion {
		t.Fatalf("未协商时应回默认协议版本头，got %q", got)
	}
}

func TestMCPToolsListAndCallExtendedTools(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)

	_, listPayload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}`)
	result, ok := listPayload["result"].(map[string]any)
	if !ok {
		t.Fatalf("tools/list 缺少 result: %+v", listPayload)
	}
	tools, ok := result["tools"].([]any)
	if !ok {
		t.Fatalf("tools 不是列表: %+v", result)
	}
	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		if n, ok := tool["name"].(string); ok {
			names[n] = true
		}
	}
	for _, expected := range []string{"list_events", "get_star_trend", "list_starred_releases", "list_outbox", "get_work_item_ai_review", "trigger_work_item_ai_review"} {
		if !names[expected] {
			t.Fatalf("tools/list 缺少工具 %q", expected)
		}
	}

	status, callPayload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"list_events","arguments":{"per_page":5}}}`)
	if status != http.StatusOK {
		t.Fatalf("list_events 状态异常: %d, %+v", status, callPayload)
	}
	text := mcpResultText(t, callPayload)
	if !strings.Contains(text, "items") {
		t.Fatalf("list_events 结果异常: %s", text)
	}

	status, outboxPayload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"list_outbox","arguments":{"status":"sent","per_page":5}}}`)
	if status != http.StatusOK {
		t.Fatalf("list_outbox 状态异常: %d, %+v", status, outboxPayload)
	}
	text = mcpResultText(t, outboxPayload)
	if !strings.Contains(text, "items") {
		t.Fatalf("list_outbox 结果异常: %s", text)
	}

	// 写入一条模拟 review 并通过 get_work_item_ai_review 工具读取
	_, _ = fixture.store.Settings().Upsert(t.Context(), store.SystemSetting{
		ID:        "s-mcp-rev-1",
		Key:       "ai.pr_review.wi-mcp-pr-1",
		ValueJSON: []byte(`{"summary":"MCP测试报告","score":88}`),
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "test",
	})

	status, reviewPayload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":13,"method":"tools/call","params":{"name":"get_work_item_ai_review","arguments":{"work_item_id":"wi-mcp-pr-1"}}}`)
	if status != http.StatusOK {
		t.Fatalf("get_work_item_ai_review 状态异常: %d, %+v", status, reviewPayload)
	}
	reviewText := mcpResultText(t, reviewPayload)
	if !strings.Contains(reviewText, "MCP测试报告") || !strings.Contains(reviewText, "88") {
		t.Fatalf("get_work_item_ai_review 结果异常: %s", reviewText)
	}
}

// TestMCPWriteTools 验证新加入的自动化运维写工具：trigger_reconciliation, retry_failed_outbox, replay_webhook_delivery。
func TestMCPWriteTools(t *testing.T) {
	fixture := oauthFixture(t)
	token := mcpAccessToken(t, fixture)
	ctx := t.Context()

	// 1. 验证 tools/list 包含这三个写工具
	_, payload := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":10,"method":"tools/list","params":{}}`)
	result := payload["result"].(map[string]any)
	tools := result["tools"].([]any)
	names := map[string]bool{}
	for _, raw := range tools {
		tool := raw.(map[string]any)
		name, _ := tool["name"].(string)
		names[name] = true
	}
	for _, want := range []string{"trigger_reconciliation", "retry_failed_outbox", "replay_webhook_delivery"} {
		if !names[want] {
			t.Fatalf("tools/list missing write tool %q: %+v", want, names)
		}
	}

	// 2. 插入一条 dead outbox 记录并测试 retry_failed_outbox
	_, _ = fixture.store.Outbox().Create(ctx, store.NotificationOutbox{
		ID:        "01JMCPDEAD0000000000000001",
		ChannelID: "ch-1",
		Status:    store.OutboxDead,
		Title:     "Dead message",
	})
	status, callResp := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":11,"method":"tools/call","params":{"name":"retry_failed_outbox","arguments":{}}}`)
	if status != http.StatusOK {
		t.Fatalf("call retry_failed_outbox status=%d", status)
	}
	callRes := callResp["result"].(map[string]any)
	content := callRes["content"].([]any)
	text, _ := content[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, "\"status\":\"queued\"") {
		t.Fatalf("unexpected call result: %s", text)
	}

	// 3. 准备一条 webhook delivery 并调用 replay_webhook_delivery
	d, _ := fixture.store.WebhookDeliveries().Create(ctx, store.WebhookDelivery{
		ID:                 "01JMCPWH000000000000000001",
		DeliveryID:         "del-mcp-01",
		EventType:          "push",
		RepositoryFullName: "test/mcp",
		Status:             store.DeliveryProcessed,
		Payload:            []byte(`{"ref":"refs/heads/main"}`),
	})
	status, replayResp := mcpRequest(t, fixture, token, `{"jsonrpc":"2.0","id":12,"method":"tools/call","params":{"name":"replay_webhook_delivery","arguments":{"id":"`+d.ID+`"}}}`)
	if status != http.StatusOK {
		t.Fatalf("call replay_webhook_delivery status=%d", status)
	}
	replayRes := replayResp["result"].(map[string]any)
	replayContent := replayRes["content"].([]any)
	replayText, _ := replayContent[0].(map[string]any)["text"].(string)
	if !strings.Contains(replayText, "\"status\":\"replayed\"") {
		t.Fatalf("unexpected replay result: %s", replayText)
	}
}

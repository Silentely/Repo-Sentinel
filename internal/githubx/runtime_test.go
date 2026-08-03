package githubx

import (
	"strings"
	"testing"
)

// TestRuntimeSnapshot 验证 Snapshot 返回独立的只读副本。
func TestRuntimeSnapshot(t *testing.T) {
	rt := &RuntimeConfig{
		AppID:          123,
		ClientID:       "Iv1.abc",
		PrivateKeyPath: "/keys/app.pem",
		WebhookSecret:  "s1",
		PublicBaseURL:  "https://example.org",
		AppIDSource:    "env",
	}
	snap := rt.Snapshot()
	if snap.AppID != 123 || snap.ClientID != "Iv1.abc" {
		t.Fatalf("snapshot 未保留字段: app_id=%d client_id=%s", snap.AppID, snap.ClientID)
	}
	// 修改快照不应影响原对象。
	snap.AppID = 999
	if rt.AppID != 123 {
		t.Fatalf("修改快照泄漏到原对象: AppID=%d", rt.AppID)
	}
}

// TestRuntimeNilSnapshot 验证 nil 接收者 Snapshot 返回零值。
func TestRuntimeNilSnapshot(t *testing.T) {
	var rt *RuntimeConfig
	if snap := rt.Snapshot(); snap.AppID != 0 || snap.ClientID != "" {
		t.Fatalf("nil Snapshot 应返回零值, got app_id=%d client_id=%s", snap.AppID, snap.ClientID)
	}
}

// TestRuntimeReplace 验证 Replace 使用新值覆盖并保留 Client 指针。
func TestRuntimeReplace(t *testing.T) {
	client := &AppClient{AppID: 1}
	rt := &RuntimeConfig{AppID: 1, Client: client}
	next := &RuntimeConfig{
		AppID:         2,
		ClientID:      "Iv1.new",
		PublicBaseURL: "https://new.example.org",
		AppIDSource:   "database",
	}
	rt.Replace(next)
	if rt.AppID != 2 || rt.ClientID != "Iv1.new" {
		t.Fatalf("Replace 未生效: %+v", rt)
	}
	if rt.Client != client {
		t.Fatal("Replace 应保留原 Client 指针")
	}
	// nil 与 nil next 应安全。
	rt.Replace(nil)
	var nilRT *RuntimeConfig
	nilRT.Replace(next)
}

// TestRuntimeWebhookSecrets 验证只返回非空 secret。
func TestRuntimeWebhookSecrets(t *testing.T) {
	rt := &RuntimeConfig{WebhookSecret: "  cur  ", WebhookPreviousSecret: "  prev  "}
	secrets := rt.WebhookSecrets()
	if len(secrets) != 2 || secrets[0] != "cur" || secrets[1] != "prev" {
		t.Fatalf("WebhookSecrets = %v, want [cur prev]", secrets)
	}
	// 空白 secret 被裁剪后不应计入。
	rt2 := &RuntimeConfig{WebhookPreviousSecret: "  "}
	if got := rt2.WebhookSecrets(); len(got) != 0 {
		t.Fatalf("空白 secret 不应返回, got %v", got)
	}
	var nilRT *RuntimeConfig
	if got := nilRT.WebhookSecrets(); got != nil {
		t.Fatalf("nil WebhookSecrets 应返回 nil, got %v", got)
	}
}

// TestRuntimeStatusFlags 验证管理面状态标记(不含秘密)。
func TestRuntimeStatusFlags(t *testing.T) {
	rt := &RuntimeConfig{
		AppID:                 7,
		ClientID:              "Iv1.client",
		PrivateKeyPEM:         "-----BEGIN RSA PRIVATE KEY-----\n...\n-----END RSA PRIVATE KEY-----",
		WebhookSecret:         "w",
		WebhookPreviousSecret: "wp",
		ExternalPAT:           "pat",
		PublicBaseURL:         "https://example.org",
	}
	appID, clientID, privateKey, webhook, previous, externalPAT, baseURL, path := rt.StatusFlags()
	if !appID || !clientID || !privateKey || !webhook || !previous || !externalPAT {
		t.Fatalf("StatusFlags 应全部为 true, got (%v,%v,%v,%v,%v,%v)", appID, clientID, privateKey, webhook, previous, externalPAT)
	}
	if baseURL != "https://example.org" || path != "/webhooks/github" {
		t.Fatalf("StatusFlags baseURL/path = %q/%q", baseURL, path)
	}

	// 空配置时 privateKey 依赖 Client.HasPrivateKeyMaterial 兜底。
	rt2 := &RuntimeConfig{Client: &AppClient{PrivateKeyPEM: "cert"}}
	_, _, key2, _, _, _, _, _ := rt2.StatusFlags()
	if !key2 {
		t.Fatal("PEM 存在时应判定 privateKey 可用")
	}

	// nil 返回默认值。
	var nilRT *RuntimeConfig
	_, _, _, _, _, _, base, path2 := nilRT.StatusFlags()
	if base != "" || path2 != "/webhooks/github" {
		t.Fatalf("nil StatusFlags 应返回默认, got base=%q path=%q", base, path2)
	}
}

// TestRuntimeApplyToClient 验证 ApplyToClient 同步 App 身份到客户端。
func TestRuntimeApplyToClient(t *testing.T) {
	client := &AppClient{}
	rt := &RuntimeConfig{AppID: 42, PrivateKeyPEM: "pem", Client: client}
	rt.ApplyToClient()
	if client.AppID != 42 || client.PrivateKeyPEM != "pem" {
		t.Fatalf("ApplyToClient 未同步, client=%+v", client)
	}
	// nil Client 与 nil 接收者应安全。
	rt2 := &RuntimeConfig{AppID: 1}
	rt2.ApplyToClient()
	var nilRT *RuntimeConfig
	nilRT.ApplyToClient()
}

// TestRuntimeTrim 验证字段的空白处理不会破坏状态判定。
func TestRuntimeTrim(t *testing.T) {
	rt := &RuntimeConfig{WebhookSecret: "   ", ClientID: "  "}
	_, _, _, webhook, _, _, _, _ := rt.StatusFlags()
	if webhook {
		t.Fatal("全空白 WebhookSecret 不应判定为已配置")
	}
	_ = strings.TrimSpace(rt.ClientID)
}

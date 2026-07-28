package httpapi

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/base64"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
)

func testHTTPKeyRing(t *testing.T) *cryptox.KeyRing {
	t.Helper()
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(bytes.Repeat([]byte{0x41}, 32))),
	})
	if err != nil {
		t.Fatal(err)
	}
	return &ring
}

func TestGitHub配置API可读写且敏感字段不回显(t *testing.T) {
	ring := testHTTPKeyRing(t)
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: ring})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	getUnauthorized := fixture.request(t, http.MethodGet, "/api/v1/github/config", "", "127.0.0.1:44001", nil, nil)
	assertAPIError(t, getUnauthorized, http.StatusUnauthorized, "unauthorized")

	getOK := fixture.request(t, http.MethodGet, "/api/v1/github/config", "", "127.0.0.1:44002", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	var view githubConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.CanEditInUI {
		t.Fatal("期望 can_edit_in_ui")
	}
	if view.WebhookSecretConfigured {
		t.Fatal("初始不应已配置 webhook secret")
	}

	key, err := rsa.GenerateKey(rand.Reader, 1024)
	if err != nil {
		t.Fatal(err)
	}
	pemBytes := pem.EncodeToMemory(&pem.Block{Type: "RSA PRIVATE KEY", Bytes: x509.MarshalPKCS1PrivateKey(key)})
	// public_base_url 在 fixture 中已由 Config 注入并标记为 env 锁定，此处不覆盖。
	body, _ := json.Marshal(map[string]any{
		"app_id":          4242,
		"client_id":       "Iv1.testclient",
		"private_key_pem": string(pemBytes),
		"webhook_secret":  "ui-webhook-secret",
	})
	putMissingCSRF := fixture.request(t, http.MethodPut, "/api/v1/github/config", string(body), "127.0.0.1:44003", cookies, nil)
	assertAPIError(t, putMissingCSRF, http.StatusForbidden, "csrf_failed")

	putOK := fixture.request(
		t, http.MethodPut, "/api/v1/github/config", string(body),
		"127.0.0.1:44004", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	if putOK.Code != http.StatusOK {
		t.Fatalf("PUT status=%d body=%s", putOK.Code, putOK.Body.String())
	}
	if err := json.Unmarshal(putOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if view.AppID != 4242 || !view.AppIDConfigured {
		t.Fatalf("app_id 未保存: %+v", view)
	}
	if view.ClientID != "Iv1.testclient" || !view.ClientIDConfigured {
		t.Fatalf("client_id 未保存: %+v", view)
	}
	if !view.PrivateKeyConfigured || !view.WebhookSecretConfigured {
		t.Fatalf("私钥或 secret 未配置: %+v", view)
	}
	if !strings.HasSuffix(view.WebhookURL, "/webhooks/github") {
		t.Fatalf("webhook_url=%q", view.WebhookURL)
	}
	raw := putOK.Body.String()
	if strings.Contains(raw, "ui-webhook-secret") || strings.Contains(raw, "BEGIN RSA") {
		t.Fatalf("响应泄漏敏感内容: %s", raw)
	}
	if view.AppIDSource != "database" || view.WebhookSecretSource != "database" {
		t.Fatalf("来源应为 database: %+v", view)
	}

	// 再次 GET 应仍显示已配置，且不回显明文。
	getAgain := fixture.request(t, http.MethodGet, "/api/v1/github/config", "", "127.0.0.1:44005", cookies, nil)
	if getAgain.Code != http.StatusOK {
		t.Fatalf("GET again status=%d", getAgain.Code)
	}
	if strings.Contains(getAgain.Body.String(), "ui-webhook-secret") {
		t.Fatal("GET 回显了 webhook secret")
	}
}

func TestGitHub配置API环境变量字段锁定(t *testing.T) {
	ring := testHTTPKeyRing(t)
	fixture := newHTTPTestFixture(t, httpTestOptions{keyRing: ring, envAppID: 99})
	fixture.bootstrapAdmin(t)
	cookies := fixture.login(t, httpTestPassword)
	csrf := cookieByName(t, cookies, CSRFCookieName)

	getOK := fixture.request(t, http.MethodGet, "/api/v1/github/config", "", "127.0.0.1:44010", cookies, nil)
	if getOK.Code != http.StatusOK {
		t.Fatalf("GET status=%d body=%s", getOK.Code, getOK.Body.String())
	}
	var view githubConfigResponse
	if err := json.Unmarshal(getOK.Body.Bytes(), &view); err != nil {
		t.Fatal(err)
	}
	if !view.AppIDLocked || view.AppID != 99 {
		t.Fatalf("期望 app_id 锁定为 99: %+v", view)
	}

	body := `{"app_id":100}`
	resp := fixture.request(
		t, http.MethodPut, "/api/v1/github/config", body,
		"127.0.0.1:44011", cookies, map[string]string{CSRFHeaderName: csrf.Value},
	)
	assertAPIError(t, resp, http.StatusConflict, "github_field_locked")
}

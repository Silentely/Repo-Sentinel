package githubx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

func TestVerifySignature(t *testing.T) {
	body := []byte(`{"action":"opened"}`)
	mac := hmac.New(sha256.New, []byte("secret-a"))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	if !VerifySignature(body, header, "secret-a") {
		t.Fatal("当前密钥应通过")
	}
	if !VerifySignature(body, header, "wrong", "secret-a") {
		t.Fatal("previous 密钥位应通过")
	}
	if VerifySignature(body, header, "wrong") {
		t.Fatal("错误密钥不应通过")
	}
	if VerifySignature(body, "sha1=abc", "secret-a") {
		t.Fatal("非 sha256 头不应通过")
	}
	if VerifySignature(body, "sha256=123", "secret-a") {
		t.Fatal("长度不足 64 hex 不应通过")
	}
	if VerifySignature(body, "sha256="+string(make([]byte, 65)), "secret-a") {
		t.Fatal("长度超 64 hex 不应通过")
	}
	if VerifySignature(body, "sha256="+strings.Repeat("z", 64), "secret-a") {
		t.Fatal("非法 hex 字符不应通过")
	}
	if VerifySignature(body, header, "  ", "") {
		t.Fatal("空白密钥不应通过")
	}
}

func BenchmarkVerifySignature(b *testing.B) {
	body := []byte(`{"action":"opened","repository":{"full_name":"octocat/Hello-World"}}`)
	mac := hmac.New(sha256.New, []byte("test-secret"))
	mac.Write(body)
	header := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	b.ReportAllocs()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if !VerifySignature(body, header, "test-secret") {
			b.Fatal("verify failed")
		}
	}
}

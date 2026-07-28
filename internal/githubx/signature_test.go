package githubx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
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
}

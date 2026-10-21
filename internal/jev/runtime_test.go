package jev

import (
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
)

func TestRuntimeEncryption(t *testing.T) {
	ring, err := cryptox.NewKeyRing(config.EncryptionConfig{CurrentKey: config.NewSecret(strings.Repeat("01", 32))})
	if err != nil {
		t.Fatal(err)
	}
	envelope, err := EncryptAPIKey(t.Context(), &ring, "fixture-key")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(envelope, "fixture-key") {
		t.Fatal("明文泄露")
	}
	plain, err := DecryptAPIKey(t.Context(), &ring, envelope)
	if err != nil || plain != "fixture-key" {
		t.Fatalf("密文往返失败: %v", err)
	}
	if _, err := ring.Decrypt(t.Context(), envelope, []byte("reposentinel:ai-runtime:v1")); err == nil {
		t.Fatal("跨 AAD 解密必须失败")
	}
}

package githubx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature 校验 X-Hub-Signature-256；支持当前与 previous secret。
func VerifySignature(body []byte, header string, secrets ...string) bool {
	header = strings.TrimSpace(header)
	if !strings.HasPrefix(header, "sha256=") {
		return false
	}
	got, err := hex.DecodeString(strings.TrimPrefix(header, "sha256="))
	if err != nil || len(got) != sha256.Size {
		return false
	}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		if hmac.Equal(got, mac.Sum(nil)) {
			return true
		}
	}
	return false
}

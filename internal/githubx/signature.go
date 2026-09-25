package githubx

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"strings"
)

// VerifySignature 校验 X-Hub-Signature-256；支持当前与 previous secret。
// 通过长度预检与栈缓冲区解码/校验，实现零堆分配签名验证。
func VerifySignature(body []byte, header string, secrets ...string) bool {
	header = strings.TrimSpace(header)
	const prefix = "sha256="
	if !strings.HasPrefix(header, prefix) {
		return false
	}
	hexPart := header[len(prefix):]
	if len(hexPart) != sha256.Size*2 {
		return false
	}
	var hexPartBytes [sha256.Size * 2]byte
	copy(hexPartBytes[:], hexPart)
	var got [sha256.Size]byte
	if _, err := hex.Decode(got[:], hexPartBytes[:]); err != nil {
		return false
	}
	for _, secret := range secrets {
		secret = strings.TrimSpace(secret)
		if secret == "" {
			continue
		}
		mac := hmac.New(sha256.New, []byte(secret))
		_, _ = mac.Write(body)
		var expected [sha256.Size]byte
		mac.Sum(expected[:0])
		if hmac.Equal(got[:], expected[:]) {
			return true
		}
	}
	return false
}

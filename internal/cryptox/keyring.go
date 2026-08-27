package cryptox

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

const (
	encryptionKeySize = 32
	keyIDSize         = 12
	keyRingMask       = "[REDACTED]"
)

// ErrInvalidEncryptionKey 主密钥格式非法（hex/base64 解码失败或长度不符）。
// 定义为包级 sentinel：app 层据此区分「格式非法」与「与库内探针不匹配」两种启动失败。
var ErrInvalidEncryptionKey = errors.New("invalid_encryption_key")

// KeyRing 保存当前与上一把数据加密密钥对应的 AEAD 实例。
// 字段保持私有，并通过格式化方法阻止密钥材料进入日志或诊断文本。
type KeyRing struct {
	current  keyMaterial
	previous keyMaterial
}

// DecryptResult 返回明文，并标记是否由上一把密钥完成解密。
type DecryptResult struct {
	Plaintext       []byte
	UsedPreviousKey bool
}

type keyMaterial struct {
	aead      cipher.AEAD
	id        string
	available bool
}

// NewKeyRing 解码配置中的 AES-256 密钥并建立当前/上一把密钥环。
func NewKeyRing(cfg config.EncryptionConfig) (KeyRing, error) {
	currentKey, err := decodeEncryptionKey(cfg.CurrentKey, true)
	if err != nil {
		return KeyRing{}, ErrInvalidEncryptionKey
	}
	current, err := newKeyMaterial(currentKey)
	if err != nil {
		return KeyRing{}, ErrInvalidEncryptionKey
	}

	previousKey, err := decodeEncryptionKey(cfg.PreviousKey, false)
	if err != nil {
		return KeyRing{}, ErrInvalidEncryptionKey
	}
	var previous keyMaterial
	if len(previousKey) > 0 {
		previous, err = newKeyMaterial(previousKey)
		if err != nil {
			return KeyRing{}, ErrInvalidEncryptionKey
		}
	}

	return KeyRing{current: current, previous: previous}, nil
}

func decodeEncryptionKey(secret config.Secret, required bool) ([]byte, error) {
	encoded := secret.Reveal()
	if encoded == "" {
		if required {
			return nil, ErrInvalidEncryptionKey
		}
		return nil, nil
	}

	if decoded, err := hex.DecodeString(encoded); err == nil && len(decoded) == encryptionKeySize {
		return decoded, nil
	}
	for _, encoding := range []*base64.Encoding{base64.StdEncoding, base64.RawStdEncoding} {
		if decoded, err := encoding.DecodeString(encoded); err == nil && len(decoded) == encryptionKeySize {
			return decoded, nil
		}
	}
	return nil, ErrInvalidEncryptionKey
}

func newKeyMaterial(key []byte) (keyMaterial, error) {
	block, err := aes.NewCipher(key)
	if err != nil {
		return keyMaterial{}, ErrInvalidEncryptionKey
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return keyMaterial{}, ErrInvalidEncryptionKey
	}
	digest := sha256.Sum256(key)
	return keyMaterial{
		aead:      aead,
		id:        hex.EncodeToString(digest[:])[:keyIDSize],
		available: true,
	}, nil
}

// DeriveHMACKey 从当前主密钥派生确定性的 HMAC 签名密钥。
// 使用固定 nonce 与固定明文：主密钥不变则派生结果不变（重启后令牌仍可验证），
// 主密钥轮换后旧派生密钥自动失效（已签发令牌同步作废）。
// 派生密钥仅用于 OAuth 访问令牌签名等次级用途，绝不外泄主密钥材料。
func (k KeyRing) DeriveHMACKey(aad []byte) ([]byte, error) {
	if !k.current.available {
		return nil, ErrInvalidEncryptionKey
	}
	var nonce [12]byte
	const plaintext = "reposentinel:derive:hmac:v1"
	sealed := k.current.aead.Seal(nil, nonce[:], []byte(plaintext), aad)
	return sealed, nil
}

// String 返回固定掩码，避免普通字符串格式化泄漏密钥环内容。
func (KeyRing) String() string {
	return keyRingMask
}

// GoString 返回固定掩码，避免 %#v 展开私有字段。
func (KeyRing) GoString() string {
	return keyRingMask
}

// Format 忽略格式动词并输出固定掩码。
func (KeyRing) Format(state fmt.State, _ rune) {
	_, _ = io.WriteString(state, keyRingMask)
}

// LogValue 避免 slog 反射展开密钥环内部字段。
func (KeyRing) LogValue() slog.Value {
	return slog.StringValue(keyRingMask)
}

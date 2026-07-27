package cryptox

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"strings"
)

const (
	envelopeVersion        = "rs1"
	maxEnvelopePayloadSize = 64 * 1024
	minimumGCMPayloadSize  = 12 + 16
)

var (
	// ErrEncryptionKeyMismatch 统一表示信封解析、密钥选择或认证失败。
	ErrEncryptionKeyMismatch  = errors.New("encryption_key_mismatch")
	errInvalidEncryptionInput = errors.New("invalid_encryption_input")
	errEncryptionFailed       = errors.New("encryption_failed")
)

// Encrypt 使用当前密钥加密凭据，并返回严格的 rs1 信封。
func (k KeyRing) Encrypt(ctx context.Context, plaintext, associatedData []byte) (string, error) {
	if err := encryptionContextError(ctx); err != nil {
		return "", err
	}
	if !k.current.available || len(plaintext) == 0 || len(associatedData) == 0 {
		return "", errInvalidEncryptionInput
	}

	nonceSize := k.current.aead.NonceSize()
	if len(plaintext) > maxEnvelopePayloadSize-nonceSize-k.current.aead.Overhead() {
		return "", errInvalidEncryptionInput
	}
	nonce := make([]byte, nonceSize)
	if _, err := rand.Read(nonce); err != nil {
		return "", errEncryptionFailed
	}

	ciphertext := k.current.aead.Seal(nil, nonce, plaintext, associatedData)
	payload := make([]byte, 0, len(nonce)+len(ciphertext))
	payload = append(payload, nonce...)
	payload = append(payload, ciphertext...)
	return envelopeVersion + "." + k.current.id + "." + base64.RawURLEncoding.EncodeToString(payload), nil
}

// Decrypt 校验并解密 rs1 信封；上一把密钥成功时返回轮换标记。
func (k KeyRing) Decrypt(ctx context.Context, envelope string, associatedData []byte) (DecryptResult, error) {
	if err := encryptionContextError(ctx); err != nil {
		return DecryptResult{}, err
	}
	if len(associatedData) == 0 {
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}

	keyID, payload, ok := parseEnvelope(envelope)
	if !ok {
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}

	switch {
	case k.current.available && keyID == k.current.id:
		return decryptWithKey(k.current, payload, associatedData, false)
	case k.previous.available && keyID == k.previous.id:
		return decryptWithKey(k.previous, payload, associatedData, true)
	default:
		if result, err := decryptWithKey(k.current, payload, associatedData, false); err == nil {
			return result, nil
		}
		if result, err := decryptWithKey(k.previous, payload, associatedData, true); err == nil {
			return result, nil
		}
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}
}

func parseEnvelope(envelope string) (string, []byte, bool) {
	maxEnvelopeSize := len(envelopeVersion) + 1 + keyIDSize + 1 + base64.RawURLEncoding.EncodedLen(maxEnvelopePayloadSize)
	if len(envelope) > maxEnvelopeSize {
		return "", nil, false
	}
	parts := strings.Split(envelope, ".")
	if len(parts) != 3 || parts[0] != envelopeVersion || !validKeyID(parts[1]) || parts[2] == "" {
		return "", nil, false
	}
	if len(parts[2]) > base64.RawURLEncoding.EncodedLen(maxEnvelopePayloadSize) {
		return "", nil, false
	}
	payload, err := base64.RawURLEncoding.Strict().DecodeString(parts[2])
	if err != nil || len(payload) < minimumGCMPayloadSize || len(payload) > maxEnvelopePayloadSize {
		return "", nil, false
	}
	if base64.RawURLEncoding.EncodeToString(payload) != parts[2] {
		return "", nil, false
	}
	return parts[1], payload, true
}

func validKeyID(value string) bool {
	if len(value) != keyIDSize {
		return false
	}
	for _, char := range value {
		if (char < '0' || char > '9') && (char < 'a' || char > 'f') {
			return false
		}
	}
	return true
}

func decryptWithKey(material keyMaterial, payload, associatedData []byte, usedPrevious bool) (DecryptResult, error) {
	if !material.available || material.aead == nil {
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}
	nonceSize := material.aead.NonceSize()
	if len(payload) < nonceSize+material.aead.Overhead() {
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}
	plaintext, err := material.aead.Open(nil, payload[:nonceSize], payload[nonceSize:], associatedData)
	if err != nil {
		return DecryptResult{}, ErrEncryptionKeyMismatch
	}
	return DecryptResult{Plaintext: plaintext, UsedPreviousKey: usedPrevious}, nil
}

func encryptionContextError(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return nil
	}
}

package auth

import (
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"io"
)

const (
	randomTokenBytes = 32
	// base64.RawURLEncoding 编码 32 字节后的确切字符长度：ceil(32 * 4 / 3) = 43
	encodedTokenLength = 43
)

// CSRFTokens 签发并验证双提交 CSRF 令牌。
type CSRFTokens struct {
	random RandomReader
}

// NewCSRFTokens 创建 CSRF 令牌服务。
func NewCSRFTokens(random RandomReader) CSRFTokens {
	if random == nil {
		random = rand.Reader
	}
	return CSRFTokens{random: random}
}

// Issue 返回原始 CSRF 令牌及供 Session 持久化的 SHA-256 哈希。
func (c CSRFTokens) Issue() (rawToken, tokenHash string, err error) {
	return issueRandomToken(c.random)
}

// Validate 对 Cookie、Header 与 Session 哈希执行双提交校验。
func (c CSRFTokens) Validate(cookieToken, headerToken, expectedHash string) error {
	if len(cookieToken) != encodedTokenLength || len(headerToken) != encodedTokenLength || len(expectedHash) != sha256.Size*2 {
		return ErrCSRFFailed
	}
	cookieRaw, err := decodeEncodedToken(cookieToken)
	if err != nil {
		return ErrCSRFFailed
	}
	headerRaw, err := decodeEncodedToken(headerToken)
	if err != nil {
		return ErrCSRFFailed
	}
	expectedDigest, err := hex.DecodeString(expectedHash)
	if err != nil || len(expectedDigest) != sha256.Size {
		return ErrCSRFFailed
	}

	cookieDigest := sha256.Sum256(cookieRaw)
	tokenMatch := subtle.ConstantTimeCompare(cookieRaw, headerRaw)
	hashMatch := subtle.ConstantTimeCompare(cookieDigest[:], expectedDigest)
	if tokenMatch&hashMatch != 1 {
		return ErrCSRFFailed
	}
	return nil
}

func issueRandomToken(random RandomReader) (rawToken, tokenHash string, err error) {
	if random == nil {
		return "", "", errors.New("random source is unavailable")
	}
	var raw [randomTokenBytes]byte
	if _, err := io.ReadFull(random, raw[:]); err != nil {
		return "", "", err
	}
	digest := sha256.Sum256(raw[:])
	return base64.RawURLEncoding.EncodeToString(raw[:]), hex.EncodeToString(digest[:]), nil
}

// hashEncodedToken 将 Base64 编码的令牌解码到栈内存并直接计算哈希，避免堆分配。
func hashEncodedToken(rawToken string) (string, error) {
	if len(rawToken) != encodedTokenLength {
		return "", errors.New("token is invalid")
	}
	var raw [randomTokenBytes]byte
	n, err := base64.RawURLEncoding.Decode(raw[:], []byte(rawToken))
	if err != nil || n != randomTokenBytes {
		return "", errors.New("token is invalid")
	}
	digest := sha256.Sum256(raw[:])
	return hex.EncodeToString(digest[:]), nil
}

func decodeEncodedToken(rawToken string) ([]byte, error) {
	if len(rawToken) != encodedTokenLength {
		return nil, errors.New("token is invalid")
	}
	raw := make([]byte, randomTokenBytes)
	n, err := base64.RawURLEncoding.Decode(raw, []byte(rawToken))
	if err != nil || n != randomTokenBytes {
		return nil, errors.New("token is invalid")
	}
	return raw, nil
}

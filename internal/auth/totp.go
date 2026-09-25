package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha1"
	"crypto/subtle"
	"encoding/base32"
	"encoding/binary"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"
)

const (
	totpSecretBytes = 20
	totpPeriod      = 30
	totpDigits      = 6
)

var b32Encoding = base32.StdEncoding.WithPadding(base32.NoPadding)

// GenerateTOTPSecret 生成 20 字节密码学安全的 Base32 编码密钥。
func GenerateTOTPSecret() (string, error) {
	buf := make([]byte, totpSecretBytes)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate totp secret: %w", err)
	}
	return b32Encoding.EncodeToString(buf), nil
}

// GenerateOTPAuthURL 构建标准 otpauth://totp/... 格式的 URL。
func GenerateOTPAuthURL(username, secret, issuer string) string {
	cleanIssuer := strings.TrimSpace(issuer)
	if cleanIssuer == "" {
		cleanIssuer = "RepoSentinel"
	}
	label := cleanIssuer + ":" + strings.TrimSpace(username)
	params := url.Values{}
	params.Set("secret", secret)
	params.Set("issuer", cleanIssuer)
	params.Set("algorithm", "SHA1")
	params.Set("digits", strconv.Itoa(totpDigits))
	params.Set("period", strconv.Itoa(totpPeriod))

	return "otpauth://totp/" + url.PathEscape(label) + "?" + params.Encode()
}

func decodeBase32Secret(secret string) ([]byte, error) {
	cleanSecret := strings.Map(func(r rune) rune {
		if r == ' ' || r == '\t' || r == '\r' || r == '\n' {
			return -1
		}
		return r
	}, secret)
	cleanSecret = strings.ToUpper(cleanSecret)
	if cleanSecret == "" {
		return nil, errors.New("empty secret")
	}

	// 1. 尝试无 padding base32 解码
	unpadded := strings.TrimRight(cleanSecret, "=")
	if key, err := b32Encoding.DecodeString(unpadded); err == nil && len(key) > 0 {
		return key, nil
	}

	// 2. 尝试标准 padding base32 解码
	padLen := (8 - (len(cleanSecret) % 8)) % 8
	padded := cleanSecret + strings.Repeat("=", padLen)
	if key, err := base32.StdEncoding.DecodeString(padded); err == nil && len(key) > 0 {
		return key, nil
	}

	return nil, errors.New("invalid base32 secret")
}

// ValidateTOTP 校验用户输入的 6 位动态验证码，允许 ±1 周期（前后 30 秒）的时钟容差。
func ValidateTOTP(secret, passcode string, t time.Time) bool {
	cleanPasscode := strings.TrimSpace(passcode)
	if len(cleanPasscode) != totpDigits {
		return false
	}
	key, err := decodeBase32Secret(secret)
	if err != nil || len(key) == 0 {
		return false
	}

	counter := t.Unix() / totpPeriod
	var passBytes [totpDigits]byte
	copy(passBytes[:], cleanPasscode)

	// 校验当前周期、前一个周期和后一个周期
	for _, offset := range [...]int64{0, -1, 1} {
		if counter+offset < 0 {
			continue
		}
		expected := calculateHOTPBytes(key, uint64(counter+offset))
		if subtle.ConstantTimeCompare(passBytes[:], expected[:]) == 1 {
			return true
		}
	}
	return false
}

// GenerateTOTPCode 计算指定时间点的 TOTP 验证码（供测试或验证使用）。
func GenerateTOTPCode(secret string, t time.Time) (string, error) {
	key, err := decodeBase32Secret(secret)
	if err != nil {
		return "", fmt.Errorf("decode secret: %w", err)
	}
	counter := t.Unix() / totpPeriod
	if counter < 0 {
		counter = 0
	}
	codeBytes := calculateHOTPBytes(key, uint64(counter))
	return string(codeBytes[:]), nil
}

func calculateHOTPBytes(key []byte, counter uint64) [totpDigits]byte {
	var buf [8]byte
	binary.BigEndian.PutUint64(buf[:], counter)

	mac := hmac.New(sha1.New, key)
	mac.Write(buf[:])
	var digestBuf [sha1.Size]byte
	digest := mac.Sum(digestBuf[:0])

	offset := digest[len(digest)-1] & 0x0f
	code := binary.BigEndian.Uint32(digest[offset:offset+4]) & 0x7fffffff
	code = code % 1000000

	var res [totpDigits]byte
	res[5] = byte('0' + code%10)
	code /= 10
	res[4] = byte('0' + code%10)
	code /= 10
	res[3] = byte('0' + code%10)
	code /= 10
	res[2] = byte('0' + code%10)
	code /= 10
	res[1] = byte('0' + code%10)
	code /= 10
	res[0] = byte('0' + code%10)
	return res
}

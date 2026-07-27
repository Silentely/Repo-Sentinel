package auth

import (
	"crypto/rand"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const (
	minimumPasswordRunes = 12
	maximumPHCLength     = 256
)

var fixedPasswordParams = PasswordParams{
	Memory:      65536,
	Iterations:  3,
	Parallelism: 2,
	SaltLength:  16,
	KeyLength:   32,
}

var errInvalidPHC = errors.New("invalid password hash")

// PasswordParams 描述 Argon2id 密码哈希的受控资源参数。
type PasswordParams struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

// PasswordHasher 使用固定 Argon2id 参数生成密码哈希，并安全验证受控范围内的 PHC。
type PasswordHasher struct{}

// NewPasswordHasher 返回使用项目固定安全参数的密码哈希器。
func NewPasswordHasher() PasswordHasher {
	return PasswordHasher{}
}

// Hash 校验密码长度并生成带随机盐的 Argon2id PHC。
func (PasswordHasher) Hash(password string) (string, error) {
	if !utf8.ValidString(password) || utf8.RuneCountInString(password) < minimumPasswordRunes {
		return "", ErrValidationFailed
	}

	salt := make([]byte, fixedPasswordParams.SaltLength)
	defer clear(salt)
	if _, err := rand.Read(salt); err != nil {
		return "", errors.New("password hashing failed")
	}

	passwordBytes := []byte(password)
	defer clear(passwordBytes)
	key := argon2.IDKey(
		passwordBytes,
		salt,
		fixedPasswordParams.Iterations,
		fixedPasswordParams.Memory,
		fixedPasswordParams.Parallelism,
		fixedPasswordParams.KeyLength,
	)
	defer clear(key)

	return fmt.Sprintf(
		"$argon2id$v=%d$m=%d,t=%d,p=%d$%s$%s",
		argon2.Version,
		fixedPasswordParams.Memory,
		fixedPasswordParams.Iterations,
		fixedPasswordParams.Parallelism,
		base64.RawStdEncoding.EncodeToString(salt),
		base64.RawStdEncoding.EncodeToString(key),
	), nil
}

// Verify 验证 PHC 结构与资源边界后，以常量时间比较密码哈希。
func (PasswordHasher) Verify(encoded, password string) (bool, error) {
	params, salt, expectedKey, err := parsePHC(encoded)
	if err != nil {
		return false, ErrInvalidCredentials
	}
	defer clear(salt)
	defer clear(expectedKey)

	passwordBytes := []byte(password)
	defer clear(passwordBytes)
	actualKey := argon2.IDKey(
		passwordBytes,
		salt,
		params.Iterations,
		params.Memory,
		params.Parallelism,
		params.KeyLength,
	)
	defer clear(actualKey)

	return subtle.ConstantTimeCompare(actualKey, expectedKey) == 1, nil
}

func parsePHC(encoded string) (PasswordParams, []byte, []byte, error) {
	if encoded == "" || len(encoded) > maximumPHCLength {
		return PasswordParams{}, nil, nil, errInvalidPHC
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[0] != "" || parts[1] != "argon2id" || parts[2] != "v=19" {
		return PasswordParams{}, nil, nil, errInvalidPHC
	}

	params, err := parseParams(parts[3])
	if err != nil {
		return PasswordParams{}, nil, nil, errInvalidPHC
	}
	salt, err := decodePHCSegment(parts[4], fixedPasswordParams.SaltLength)
	if err != nil {
		return PasswordParams{}, nil, nil, errInvalidPHC
	}
	key, err := decodePHCSegment(parts[5], fixedPasswordParams.KeyLength)
	if err != nil {
		clear(salt)
		return PasswordParams{}, nil, nil, errInvalidPHC
	}
	params.SaltLength = uint32(len(salt))
	params.KeyLength = uint32(len(key))
	return params, salt, key, nil
}

func parseParams(encoded string) (PasswordParams, error) {
	fields := strings.Split(encoded, ",")
	if len(fields) != 3 {
		return PasswordParams{}, errInvalidPHC
	}

	values := make(map[string]uint64, len(fields))
	for _, field := range fields {
		name, rawValue, found := strings.Cut(field, "=")
		if !found || !validDecimal(rawValue) {
			return PasswordParams{}, errInvalidPHC
		}
		if name != "m" && name != "t" && name != "p" {
			return PasswordParams{}, errInvalidPHC
		}
		if _, duplicate := values[name]; duplicate {
			return PasswordParams{}, errInvalidPHC
		}
		value, err := strconv.ParseUint(rawValue, 10, 32)
		if err != nil {
			return PasswordParams{}, errInvalidPHC
		}
		values[name] = value
	}

	memory := values["m"]
	iterations := values["t"]
	parallelism := values["p"]
	if memory == 0 || memory > uint64(fixedPasswordParams.Memory) ||
		iterations == 0 || iterations > uint64(fixedPasswordParams.Iterations) ||
		parallelism == 0 || parallelism > uint64(fixedPasswordParams.Parallelism) ||
		memory < 8*parallelism {
		return PasswordParams{}, errInvalidPHC
	}

	return PasswordParams{
		Memory:      uint32(memory),
		Iterations:  uint32(iterations),
		Parallelism: uint8(parallelism),
	}, nil
}

func validDecimal(value string) bool {
	if value == "" {
		return false
	}
	for _, char := range value {
		if char < '0' || char > '9' {
			return false
		}
	}
	return true
}

func decodePHCSegment(encoded string, maximumLength uint32) ([]byte, error) {
	maximumEncodedLength := base64.RawStdEncoding.EncodedLen(int(maximumLength))
	if encoded == "" || len(encoded) > maximumEncodedLength {
		return nil, errInvalidPHC
	}

	decoded := make([]byte, base64.RawStdEncoding.DecodedLen(len(encoded)))
	encodedBytes := []byte(encoded)
	defer clear(encodedBytes)
	n, err := base64.RawStdEncoding.Decode(decoded, encodedBytes)
	if err != nil || n == 0 || n > int(maximumLength) {
		clear(decoded)
		return nil, errInvalidPHC
	}
	return decoded[:n], nil
}

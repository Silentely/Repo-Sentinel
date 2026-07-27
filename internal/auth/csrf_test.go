package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"testing"
)

func TestCSRF签发并验证CookieHeader与Session哈希(t *testing.T) {
	randomBytes := sequentialBytes(32)
	tokens := NewCSRFTokens(bytes.NewReader(randomBytes))
	rawToken, tokenHash, err := tokens.Issue()
	if err != nil {
		t.Fatalf("签发 CSRF 令牌失败: %v", err)
	}
	digest := sha256.Sum256(randomBytes)
	if tokenHash != hex.EncodeToString(digest[:]) {
		t.Fatal("CSRF 签发结果必须返回原始令牌的 SHA-256 哈希")
	}
	if err := tokens.Validate(rawToken, rawToken, tokenHash); err != nil {
		t.Fatalf("匹配的 Cookie/Header/Session 哈希应通过，实际错误=%v", err)
	}
}

func TestCSRF缺失畸形或不匹配统一返回稳定错误(t *testing.T) {
	tokens := NewCSRFTokens(bytes.NewReader(sequentialBytes(32)))
	rawToken, tokenHash, err := tokens.Issue()
	if err != nil {
		t.Fatalf("签发 CSRF 令牌失败: %v", err)
	}
	otherToken, _, err := NewCSRFTokens(bytes.NewReader(bytes.Repeat([]byte{0xff}, 32))).Issue()
	if err != nil {
		t.Fatalf("签发另一个 CSRF 令牌失败: %v", err)
	}

	cases := map[string]struct {
		cookie string
		header string
		hash   string
	}{
		"缺失Cookie":        {header: rawToken, hash: tokenHash},
		"缺失Header":        {cookie: rawToken, hash: tokenHash},
		"缺失Session哈希":     {cookie: rawToken, header: rawToken},
		"畸形Cookie":        {cookie: "%%%", header: rawToken, hash: tokenHash},
		"畸形Header":        {cookie: rawToken, header: "%%%", hash: tokenHash},
		"CookieHeader不匹配": {cookie: rawToken, header: otherToken, hash: tokenHash},
		"Session哈希不匹配":    {cookie: rawToken, header: rawToken, hash: string(bytes.Repeat([]byte{'0'}, 64))},
	}
	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			err := tokens.Validate(testCase.cookie, testCase.header, testCase.hash)
			if !errors.Is(err, ErrCSRFFailed) || err.Error() != "csrf_failed" {
				t.Fatalf("CSRF 错误=%v，期望统一 csrf_failed", err)
			}
		})
	}
}

func TestCSRF随机源不足时不返回部分令牌(t *testing.T) {
	tokens := NewCSRFTokens(bytes.NewReader(sequentialBytes(31)))
	rawToken, tokenHash, err := tokens.Issue()
	if err == nil {
		t.Fatal("随机源不足时签发应失败")
	}
	if rawToken != "" || tokenHash != "" {
		t.Fatal("随机源失败时不得返回部分 CSRF 令牌或哈希")
	}
}

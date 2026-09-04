package auth

import (
	"strings"
	"testing"
	"time"
)

func TestTOTPGenerationAndValidation(t *testing.T) {
	secret, err := GenerateTOTPSecret()
	if err != nil {
		t.Fatalf("GenerateTOTPSecret 失败: %v", err)
	}
	if len(secret) == 0 {
		t.Fatal("生成的 secret 为空")
	}

	url := GenerateOTPAuthURL("admin", secret, "RepoSentinel")
	if !strings.HasPrefix(url, "otpauth://totp/RepoSentinel:admin?") {
		t.Fatalf("url 格式错误: %s", url)
	}
	if !strings.Contains(url, "secret="+secret) {
		t.Fatalf("url 缺少 secret 参数: %s", url)
	}

	now := time.Unix(1700000000, 0)
	code, err := GenerateTOTPCode(secret, now)
	if err != nil {
		t.Fatalf("GenerateTOTPCode 失败: %v", err)
	}
	if len(code) != 6 {
		t.Fatalf("生成的 code 长度不为 6: %s", code)
	}

	// 当前时间点验证成功
	if !ValidateTOTP(secret, code, now) {
		t.Fatal("当前时间点应验证通过")
	}

	// 前一个周期（-30s）容差验证成功
	if !ValidateTOTP(secret, code, now.Add(30*time.Second)) {
		t.Fatal("+30s 时钟偏移应验证通过")
	}

	// 后一个周期（+30s）容差验证成功
	if !ValidateTOTP(secret, code, now.Add(-30*time.Second)) {
		t.Fatal("-30s 时钟偏移应验证通过")
	}

	// 超出容差范围（-60s 或 +60s）拒绝
	if ValidateTOTP(secret, code, now.Add(65*time.Second)) {
		t.Fatal("+65s 时钟偏移应拒绝")
	}
	if ValidateTOTP(secret, code, now.Add(-65*time.Second)) {
		t.Fatal("-65s 时钟偏移应拒绝")
	}

	// 错误格式验证码拒绝
	if ValidateTOTP(secret, "12345", now) {
		t.Fatal("小于 6 位数字应拒绝")
	}
	if ValidateTOTP(secret, "1234567", now) {
		t.Fatal("大于 6 位数字应拒绝")
	}
	if ValidateTOTP(secret, "abcdef", now) {
		t.Fatal("错误 code 应拒绝")
	}
}

func TestRFC6238ReferenceVectors(t *testing.T) {
	// RFC 6238 Appendix B 针对 SHA1 的参考密钥 "12345678901234567890" (ASCII, 20 bytes)
	// Base32 编码为 GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ
	secret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"

	vectors := []struct {
		timestamp int64
		expected  string
	}{
		{59, "287082"},
		{1111111109, "081804"},
		{1111111111, "050471"},
		{1234567890, "005924"},
		{2000000000, "279037"},
	}

	for _, v := range vectors {
		code, err := GenerateTOTPCode(secret, time.Unix(v.timestamp, 0))
		if err != nil {
			t.Fatalf("t=%d 计算失败: %v", v.timestamp, err)
		}
		if code != v.expected {
			t.Errorf("t=%d: got %s, want %s", v.timestamp, code, v.expected)
		}
		if !ValidateTOTP(secret, v.expected, time.Unix(v.timestamp, 0)) {
			t.Errorf("t=%d: ValidateTOTP 失败", v.timestamp)
		}
	}
}

func TestTOTPSecretFormatRobustness(t *testing.T) {
	now := time.Unix(1700000000, 0)
	rawSecret := "GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ"
	expectedCode, err := GenerateTOTPCode(rawSecret, now)
	if err != nil {
		t.Fatalf("生成参考验证码失败: %v", err)
	}

	variants := []string{
		"GEZD GNBV GY3T QOJQ GEZD GNBV GY3T QOJQ", // 带空格
		"gezdgnbvgy3tqojqgezdgnbvgy3tqojq",       // 小写
		"GEZDGNBVGY3TQOJQGEZDGNBVGY3TQOJQ======", // 带 padding
		"  GEZD GNBV \t\n gy3t qojq GEZDGNBVGY3TQOJQ  ", // 混杂空白符与大小写
	}

	for _, variant := range variants {
		code, err := GenerateTOTPCode(variant, now)
		if err != nil {
			t.Errorf("变体 %q GenerateTOTPCode 失败: %v", variant, err)
			continue
		}
		if code != expectedCode {
			t.Errorf("变体 %q 计算出的 code=%s，期望 %s", variant, code, expectedCode)
		}
		if !ValidateTOTP(variant, expectedCode, now) {
			t.Errorf("变体 %q ValidateTOTP 验证失败", variant)
		}
	}
}

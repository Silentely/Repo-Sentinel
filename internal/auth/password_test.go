package auth

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

const (
	testPassword      = "管理员安全密码一二三四五六七八"
	testWrongPassword = "管理员错误密码一二三四五六七八"
)

// 此测试能捕获 Hash 参数漂移、PHC 格式错误或重复使用盐导致相同密码产生相同哈希。
func TestArgon2id密码哈希使用固定参数与随机盐(t *testing.T) {
	hasher := NewPasswordHasher()
	first, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("首次哈希密码失败: %v", err)
	}
	second, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("再次哈希密码失败: %v", err)
	}

	const wantPrefix = "$argon2id$v=19$m=65536,t=3,p=2$"
	if !strings.HasPrefix(first, wantPrefix) || !strings.HasPrefix(second, wantPrefix) {
		t.Fatal("Argon2id PHC 未使用固定版本与参数前缀")
	}
	if first == second {
		t.Fatal("相同密码的两次哈希相同，随机盐未生效")
	}
	if strings.Contains(first, testPassword) || strings.Contains(second, testPassword) {
		t.Fatal("PHC 不得包含明文密码")
	}
}

// 此测试能捕获 Verify 颠倒参数、错误密码返回内部细节，或未使用常量时间结果比较的外显行为错误。
func Test密码验证区分正确与错误输入(t *testing.T) {
	hasher := NewPasswordHasher()
	encoded, err := hasher.Hash(testPassword)
	if err != nil {
		t.Fatalf("准备密码哈希失败: %v", err)
	}

	verified, err := hasher.Verify(encoded, testPassword)
	if err != nil || !verified {
		t.Fatalf("正确密码验证结果=(%v, %v)，期望通过", verified, err)
	}
	verified, err = hasher.Verify(encoded, testWrongPassword)
	if err != nil || verified {
		t.Fatalf("错误密码验证结果=(%v, %v)，期望 false 且无错误细节", verified, err)
	}
}

// 此测试能捕获按字节而非 Unicode 字符计数，或未执行十二字符最小长度策略。
func Test密码最少需要十二个Unicode字符(t *testing.T) {
	hasher := NewPasswordHasher()
	for _, password := range []string{"12345678901", "一二三四五六七八九十甲"} {
		if _, err := hasher.Hash(password); !errors.Is(err, ErrValidationFailed) {
			t.Fatalf("不足十二个字符的密码错误=%v，期望 validation_failed", err)
		}
	}
	if _, err := hasher.Hash("123456789012"); err != nil {
		t.Fatalf("恰好十二个字符的密码应通过校验: %v", err)
	}
}

// 此测试能捕获 Verify 把安全下限内的旧参数误判为损坏 PHC，阻断受控参数升级。
func TestArgon2id密码验证接受安全下限到固定上限内的参数(t *testing.T) {
	hasher := NewPasswordHasher()
	const lowerBoundPHC = "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$AA"

	verified, err := hasher.Verify(lowerBoundPHC, "safe-password")
	if err != nil || verified {
		t.Fatalf("安全下限 PHC 验证结果=(%v, %v)，期望正常比较后不匹配", verified, err)
	}
}

// 此测试能捕获畸形、超上限或不安全 PHC 在边界校验前进入 Argon2 并造成 panic 或资源耗尽。
func TestArgon2id密码验证安全拒绝畸形与超限PHC(t *testing.T) {
	hasher := NewPasswordHasher()
	cases := []struct {
		name    string
		encoded string
	}{
		{name: "空输入", encoded: ""},
		{name: "非PHC文本", encoded: "not-a-phc"},
		{name: "算法错误", encoded: "$argon2i$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "版本缺失", encoded: "$argon2id$m=8,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "版本过旧", encoded: "$argon2id$v=18$m=8,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "段数错误", encoded: "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg"},
		{name: "参数名错误", encoded: "$argon2id$v=19$x=8,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "参数重复", encoded: "$argon2id$v=19$m=8,m=8,p=1$MTIzNDU2Nzg$AA"},
		{name: "参数非数字", encoded: "$argon2id$v=19$m=eight,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "内存为零", encoded: "$argon2id$v=19$m=0,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "迭代为零", encoded: "$argon2id$v=19$m=8,t=0,p=1$MTIzNDU2Nzg$AA"},
		{name: "并行度为零", encoded: "$argon2id$v=19$m=8,t=1,p=0$MTIzNDU2Nzg$AA"},
		{name: "内存低于并行度安全下限", encoded: "$argon2id$v=19$m=15,t=1,p=2$MTIzNDU2Nzg$AA"},
		{name: "内存超过固定上限", encoded: "$argon2id$v=19$m=65537,t=1,p=1$MTIzNDU2Nzg$AA"},
		{name: "迭代超过固定上限", encoded: "$argon2id$v=19$m=8,t=4,p=1$MTIzNDU2Nzg$AA"},
		{name: "并行度超过固定上限", encoded: "$argon2id$v=19$m=24,t=1,p=3$MTIzNDU2Nzg$AA"},
		{name: "盐为空", encoded: "$argon2id$v=19$m=8,t=1,p=1$$AA"},
		{name: "密钥为空", encoded: "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$"},
		{name: "盐编码非法", encoded: "$argon2id$v=19$m=8,t=1,p=1$***$AA"},
		{name: "密钥编码非法", encoded: "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$***"},
		{name: "盐解码后超过十六字节", encoded: "$argon2id$v=19$m=8,t=1,p=1$" + strings.Repeat("A", 23) + "$AA"},
		{name: "密钥解码后超过三十二字节", encoded: "$argon2id$v=19$m=8,t=1,p=1$MTIzNDU2Nzg$" + strings.Repeat("A", 44)},
		{name: "盐编码长度无界", encoded: "$argon2id$v=19$m=8,t=1,p=1$" + strings.Repeat("A", 1024) + "$AA"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			verified, err := hasher.Verify(tc.encoded, testPassword)
			if verified || !errors.Is(err, ErrInvalidCredentials) {
				t.Fatalf("畸形 PHC 验证结果=(%v, %v)，期望 invalid_credentials", verified, err)
			}
		})
	}
}

// 此测试能捕获公共认证错误无法被 errors.Is 或稳定 ErrorCode 映射。
func Test密码错误支持稳定代码与errorsIs(t *testing.T) {
	cases := []struct {
		name string
		err  error
		code string
	}{
		{name: "无效凭据", err: ErrInvalidCredentials, code: "invalid_credentials"},
		{name: "冲突", err: ErrConflict, code: "conflict"},
		{name: "校验失败", err: ErrValidationFailed, code: "validation_failed"},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			wrapped := fmt.Errorf("wrapped: %w", tc.err)
			if !errors.Is(wrapped, tc.err) {
				t.Fatal("包装后的错误无法通过 errors.Is 判断")
			}
			coded, ok := tc.err.(interface{ ErrorCode() string })
			if !ok || coded.ErrorCode() != tc.code {
				t.Fatalf("错误码=%q，期望 %q", codedErrorCode(coded, ok), tc.code)
			}
			if tc.err.Error() != tc.code {
				t.Fatalf("安全错误文本=%q，期望稳定代码", tc.err.Error())
			}
		})
	}
}

func codedErrorCode(coded interface{ ErrorCode() string }, ok bool) string {
	if !ok {
		return ""
	}
	return coded.ErrorCode()
}

package cryptox

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/config"
)

func testKey(seed byte) []byte {
	return bytes.Repeat([]byte{seed}, 32)
}

func testEncryptionConfig(current, previous []byte) config.EncryptionConfig {
	cfg := config.EncryptionConfig{
		CurrentKey: config.NewSecret(base64.RawStdEncoding.EncodeToString(current)),
	}
	if len(previous) > 0 {
		cfg.PreviousKey = config.NewSecret(hex.EncodeToString(previous))
	}
	return cfg
}

func expectedKeyID(key []byte) string {
	sum := sha256.Sum256(key)
	return hex.EncodeToString(sum[:])[:12]
}

func newTestKeyRing(t *testing.T) KeyRing {
	t.Helper()
	ring, err := NewKeyRing(testEncryptionConfig(testKey(0x11), testKey(0x22)))
	if err != nil {
		t.Fatalf("创建测试密钥环失败: %v", err)
	}
	return ring
}

func Test当前密钥往返并输出严格rs1信封(t *testing.T) {
	current := testKey(0x11)
	ring, err := NewKeyRing(testEncryptionConfig(current, nil))
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}

	plaintext := []byte("credential-value-α")
	associatedData := []byte("github-token:record-42")
	envelope, err := ring.Encrypt(context.Background(), plaintext, associatedData)
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}

	parts := strings.Split(envelope, ".")
	if len(parts) != 3 || parts[0] != "rs1" {
		t.Fatalf("信封格式不符合 rs1 三段格式: %q", envelope)
	}
	if parts[1] != expectedKeyID(current) {
		t.Fatalf("信封 key-id=%q，期望=%q", parts[1], expectedKeyID(current))
	}
	if len(parts[1]) != 12 || strings.ToLower(parts[1]) != parts[1] {
		t.Fatalf("key-id 不是 12 位小写十六进制: %q", parts[1])
	}
	payload, err := base64.RawURLEncoding.DecodeString(parts[2])
	if err != nil {
		t.Fatalf("信封 payload 不是无填充 Base64URL: %v", err)
	}
	if strings.Contains(parts[2], "=") {
		t.Fatalf("信封 payload 不应包含 Base64 填充: %q", parts[2])
	}
	if len(payload) <= len(plaintext) {
		t.Fatalf("信封 payload 未包含 nonce 与认证标签: %d", len(payload))
	}

	result, err := ring.Decrypt(context.Background(), envelope, associatedData)
	if err != nil {
		t.Fatalf("解密失败: %v", err)
	}
	if !bytes.Equal(result.Plaintext, plaintext) {
		t.Fatalf("解密明文不一致: %q", result.Plaintext)
	}
	if result.UsedPreviousKey {
		t.Fatal("当前密钥解密不应标记 UsedPreviousKey")
	}
}

func Test每次加密生成不同随机nonce(t *testing.T) {
	ring := newTestKeyRing(t)
	ad := []byte("record:nonce")
	first, err := ring.Encrypt(context.Background(), []byte("same-value"), ad)
	if err != nil {
		t.Fatalf("第一次加密失败: %v", err)
	}
	second, err := ring.Encrypt(context.Background(), []byte("same-value"), ad)
	if err != nil {
		t.Fatalf("第二次加密失败: %v", err)
	}
	if first == second {
		t.Fatal("相同输入的两次加密产生了相同信封")
	}
}

func Test上一把密钥可解密并标记来源(t *testing.T) {
	current := testKey(0x11)
	previous := testKey(0x22)
	ring, err := NewKeyRing(testEncryptionConfig(current, previous))
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}

	// 先以只有旧密钥的环生成合法信封，再由双密钥环解密，避免测试依赖私有构造细节。
	oldRing, err := NewKeyRing(testEncryptionConfig(previous, nil))
	if err != nil {
		t.Fatalf("创建旧密钥环失败: %v", err)
	}
	plaintext := []byte("previous-credential")
	ad := []byte("record:previous")
	envelope, err := oldRing.Encrypt(context.Background(), plaintext, ad)
	if err != nil {
		t.Fatalf("旧密钥加密失败: %v", err)
	}
	result, err := ring.Decrypt(context.Background(), envelope, ad)
	if err != nil {
		t.Fatalf("上一把密钥解密失败: %v", err)
	}
	if !bytes.Equal(result.Plaintext, plaintext) || !result.UsedPreviousKey {
		t.Fatalf("上一把密钥结果错误: plaintext=%q usedPrevious=%v", result.Plaintext, result.UsedPreviousKey)
	}
}

func Test未知keyID按当前再上一把密钥回退(t *testing.T) {
	current := testKey(0x11)
	previous := testKey(0x22)
	ring, err := NewKeyRing(testEncryptionConfig(current, previous))
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}

	ad := []byte("record:future-id")
	currentEnvelope, err := ring.Encrypt(context.Background(), []byte("future-current"), ad)
	if err != nil {
		t.Fatalf("当前密钥加密失败: %v", err)
	}
	unknownCurrent := strings.Replace(currentEnvelope, expectedKeyID(current), "abcdefabcdef", 1)
	result, err := ring.Decrypt(context.Background(), unknownCurrent, ad)
	if err != nil || string(result.Plaintext) != "future-current" || result.UsedPreviousKey {
		t.Fatalf("未知 id 回退当前密钥失败: result=%q usedPrevious=%v err=%v", result.Plaintext, result.UsedPreviousKey, err)
	}

	oldRing, err := NewKeyRing(testEncryptionConfig(previous, nil))
	if err != nil {
		t.Fatalf("创建旧密钥环失败: %v", err)
	}
	previousEnvelope, err := oldRing.Encrypt(context.Background(), []byte("future-previous"), ad)
	if err != nil {
		t.Fatalf("旧密钥加密失败: %v", err)
	}
	unknownPrevious := strings.Replace(previousEnvelope, expectedKeyID(previous), "abcdefabcdef", 1)
	result, err = ring.Decrypt(context.Background(), unknownPrevious, ad)
	if err != nil || string(result.Plaintext) != "future-previous" || !result.UsedPreviousKey {
		t.Fatalf("未知 id 回退上一把密钥失败: result=%q usedPrevious=%v err=%v", result.Plaintext, result.UsedPreviousKey, err)
	}
}

func Test精确命中keyID时不跨密钥回退(t *testing.T) {
	current := testKey(0x11)
	previous := testKey(0x22)
	ring, err := NewKeyRing(testEncryptionConfig(current, previous))
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}
	oldRing, err := NewKeyRing(testEncryptionConfig(previous, nil))
	if err != nil {
		t.Fatalf("创建旧密钥环失败: %v", err)
	}
	envelope, err := oldRing.Encrypt(context.Background(), []byte("exact-id-no-fallback"), []byte("record:exact"))
	if err != nil {
		t.Fatalf("旧密钥加密失败: %v", err)
	}
	forgedID := strings.Replace(envelope, expectedKeyID(previous), expectedKeyID(current), 1)
	_, err = ring.Decrypt(context.Background(), forgedID, []byte("record:exact"))
	requireKeyMismatch(t, err)
}

func Test错误关联数据映射为稳定密钥不匹配(t *testing.T) {
	ring := newTestKeyRing(t)
	secretMarker := "wrong-ad-secret-marker"
	envelope, err := ring.Encrypt(context.Background(), []byte("credential-marker"), []byte("record:right"))
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	_, err = ring.Decrypt(context.Background(), envelope, []byte(secretMarker))
	requireKeyMismatch(t, err)
	if err.Error() != "encryption_key_mismatch" || strings.Contains(err.Error(), secretMarker) {
		t.Fatalf("错误文本不稳定或泄漏关联数据: %q", err)
	}
}

func Test空明文和空关联数据均被拒绝(t *testing.T) {
	ring := newTestKeyRing(t)
	if _, err := ring.Encrypt(context.Background(), nil, []byte("record:empty")); err == nil {
		t.Fatal("空明文未被拒绝")
	}
	if _, err := ring.Encrypt(context.Background(), []byte("non-empty"), nil); err == nil {
		t.Fatal("空关联数据未被拒绝")
	}
	if _, err := ring.Encrypt(context.Background(), []byte("non-empty"), []byte{}); err == nil {
		t.Fatal("零长度关联数据未被拒绝")
	}
}

func Test加密输入过大以免产生不可解密信封(t *testing.T) {
	ring := newTestKeyRing(t)
	if _, err := ring.Encrypt(context.Background(), bytes.Repeat([]byte{'x'}, 64*1024), []byte("record:large")); err == nil {
		t.Fatal("过大明文未被拒绝")
	}
}

func Test解密拒绝空关联数据(t *testing.T) {
	ring := newTestKeyRing(t)
	envelope, err := ring.Encrypt(context.Background(), []byte("credential"), []byte("record:required-ad"))
	if err != nil {
		t.Fatalf("加密失败: %v", err)
	}
	if _, err := ring.Decrypt(context.Background(), envelope, nil); err == nil {
		t.Fatal("空关联数据未被解密端拒绝")
	}
}

func Test畸形截断非法Base64和过大信封安全失败(t *testing.T) {
	ring := newTestKeyRing(t)
	keyID := expectedKeyID(testKey(0x11))
	largePayload := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'z'}, 64*1024+1))
	large := "rs1." + keyID + "." + largePayload
	cases := []string{
		"",
		"rs1",
		"rs1." + keyID,
		"v2." + keyID + ".AAAA",
		"rs1." + keyID + ".not_base64!",
		"rs1." + keyID + "." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'a'}, 12)),
		"rs1." + strings.ToUpper(keyID) + ".AAAA",
		large,
	}
	for _, envelope := range cases {
		t.Run(fmt.Sprintf("信封_%d", len(envelope)), func(t *testing.T) {
			defer func() {
				if recovered := recover(); recovered != nil {
					t.Fatalf("畸形信封触发 panic: %v", recovered)
				}
			}()
			_, err := ring.Decrypt(context.Background(), envelope, []byte("record:malformed"))
			requireKeyMismatch(t, err)
		})
	}
}

func Test未知密钥和不可用上一把密钥统一返回哨兵(t *testing.T) {
	ring, err := NewKeyRing(testEncryptionConfig(testKey(0x11), nil))
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}
	envelope := "rs1.ffffffffffff." + base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{'a'}, 32))
	_, err = ring.Decrypt(context.Background(), envelope, []byte("record:unknown-key"))
	requireKeyMismatch(t, err)
}

func Test构造器支持标准Base64无填充Base64和Hex并拒绝缺失无效密钥(t *testing.T) {
	key := testKey(0x33)
	encodings := []string{
		base64.StdEncoding.EncodeToString(key),
		base64.RawStdEncoding.EncodeToString(key),
		hex.EncodeToString(key),
	}
	for index, encoded := range encodings {
		t.Run(fmt.Sprintf("编码_%d", index), func(t *testing.T) {
			cfg := config.EncryptionConfig{CurrentKey: config.NewSecret(encoded)}
			if _, err := NewKeyRing(cfg); err != nil {
				t.Fatalf("合法密钥编码被拒绝: %v", err)
			}
		})
	}
	for name, cfg := range map[string]config.EncryptionConfig{
		"缺失当前密钥": {},
		"无效当前密钥": {CurrentKey: config.NewSecret("constructor-invalid-current")},
		"无效上一把密钥": {
			CurrentKey:  config.NewSecret(hex.EncodeToString(key)),
			PreviousKey: config.NewSecret("constructor-invalid-previous"),
		},
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := NewKeyRing(cfg); err == nil {
				t.Fatal("无效密钥配置未被构造器拒绝")
			}
		})
	}
}

func Test取消上下文时透传ctx错误(t *testing.T) {
	ring := newTestKeyRing(t)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := ring.Encrypt(cancelled, []byte("credential"), []byte("record:cancel")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Encrypt 未透传 context.Canceled: %v", err)
	}
	if _, err := ring.Decrypt(cancelled, "rs1.ffffffffffff.AAAA", []byte("record:cancel")); !errors.Is(err, context.Canceled) {
		t.Fatalf("Decrypt 未透传 context.Canceled: %v", err)
	}
}

func Test密钥环格式化始终掩码且错误不泄漏输入(t *testing.T) {
	keyMarker := "format-key-marker-1234567890"
	plaintextMarker := "format-plaintext-marker-1234567890"
	ring, err := NewKeyRing(config.EncryptionConfig{CurrentKey: config.NewSecret(hex.EncodeToString(testKey(0x44)))})
	if err != nil {
		t.Fatalf("创建密钥环失败: %v", err)
	}
	formatted := fmt.Sprintf("%v|%+v|%#v|%s|%q", ring, ring, ring, ring, ring)
	if strings.Contains(formatted, keyMarker) || strings.Contains(formatted, plaintextMarker) || strings.Contains(formatted, "[44") {
		t.Fatalf("密钥环格式化疑似泄漏敏感内容: %q", formatted)
	}

	_, err = ring.Decrypt(context.Background(), "rs1.000000000000.not-base64-"+plaintextMarker, []byte("ad-"+keyMarker))
	requireKeyMismatch(t, err)
	if strings.Contains(err.Error(), keyMarker) || strings.Contains(err.Error(), plaintextMarker) {
		t.Fatalf("密码学错误泄漏输入: %q", err)
	}
}

func TestDeriveHMACKey确定性且随主密钥轮换(t *testing.T) {
	ring := newTestKeyRing(t)
	aad := []byte("oauth:signing:v1")

	first, err := ring.DeriveHMACKey(aad)
	if err != nil {
		t.Fatalf("首次派生失败: %v", err)
	}
	if len(first) == 0 {
		t.Fatal("派生密钥为空")
	}
	second, err := ring.DeriveHMACKey(aad)
	if err != nil {
		t.Fatalf("二次派生失败: %v", err)
	}
	if !bytes.Equal(first, second) {
		t.Fatal("相同主密钥与 AAD 的派生结果不稳定")
	}
	if bytes.Equal(first, testKey(0x11)) {
		t.Fatal("派生密钥不应等于主密钥材料")
	}

	otherRing, err := NewKeyRing(testEncryptionConfig(testKey(0x99), nil))
	if err != nil {
		t.Fatalf("创建另一密钥环失败: %v", err)
	}
	third, err := otherRing.DeriveHMACKey(aad)
	if err != nil {
		t.Fatalf("他钥派生失败: %v", err)
	}
	if bytes.Equal(first, third) {
		t.Fatal("不同主密钥不应派生相同密钥")
	}

	// 不同 AAD 也应产生不同派生密钥（防止跨用途复用同一签名密钥）。
	fourth, err := ring.DeriveHMACKey([]byte("oauth:signing:v2"))
	if err != nil {
		t.Fatalf("异 AAD 派生失败: %v", err)
	}
	if bytes.Equal(first, fourth) {
		t.Fatal("不同 AAD 不应派生相同密钥")
	}
}

func requireKeyMismatch(t *testing.T, err error) {
	t.Helper()
	if !errors.Is(err, ErrEncryptionKeyMismatch) {
		t.Fatalf("错误未映射到 ErrEncryptionKeyMismatch: %v", err)
	}
	if err.Error() != "encryption_key_mismatch" {
		t.Fatalf("错误文本不稳定: %q", err)
	}
}

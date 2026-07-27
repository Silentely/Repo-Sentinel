package auth

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"reflect"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

func TestSession创建仅返回原始令牌并持久化哈希(t *testing.T) {
	opened := openAuthStore(t)
	now := time.Date(2026, 7, 27, 13, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	randomBytes := sequentialBytes(64)
	service := NewSessionService(opened, clock, bytes.NewReader(randomBytes), 0)
	adminID := createSessionTestAdmin(t, opened, now)

	created, err := service.Create(
		t.Context(),
		adminID,
		"127.0.0.1:43210",
		strings.Repeat("a", 600),
	)
	if err != nil {
		t.Fatalf("创建 Session 失败: %v", err)
	}

	rawToken, err := base64.RawURLEncoding.DecodeString(created.Token)
	if err != nil || len(rawToken) != 32 || !bytes.Equal(rawToken, randomBytes[:32]) {
		t.Fatalf("Session 原始令牌不是预期的 32 随机字节: 长度=%d, 错误=%v", len(rawToken), err)
	}
	rawCSRF, err := base64.RawURLEncoding.DecodeString(created.CSRFToken)
	if err != nil || len(rawCSRF) != 32 || !bytes.Equal(rawCSRF, randomBytes[32:]) {
		t.Fatalf("CSRF 原始令牌不是预期的 32 随机字节: 长度=%d, 错误=%v", len(rawCSRF), err)
	}

	tokenDigest := sha256.Sum256(rawToken)
	csrfDigest := sha256.Sum256(rawCSRF)
	tokenHash := hex.EncodeToString(tokenDigest[:])
	csrfHash := hex.EncodeToString(csrfDigest[:])
	stored, err := opened.Sessions().GetActiveByTokenHash(t.Context(), tokenHash, now)
	if err != nil {
		t.Fatalf("使用 Session 令牌哈希读取持久化记录失败: %v", err)
	}
	if stored.ID != created.Session.ID || stored.AdminID != adminID {
		t.Fatalf("持久化 Session 标识不匹配: %+v", stored)
	}
	if stored.TokenHash != tokenHash || stored.TokenHash == created.Token {
		t.Fatal("持久化层必须只保存 Session 令牌 SHA-256 哈希")
	}
	if stored.CSRFHash != csrfHash || stored.CSRFHash == created.CSRFToken {
		t.Fatal("持久化层必须只保存 CSRF 令牌 SHA-256 哈希")
	}
	if !stored.CreatedAt.Equal(now) || !stored.LastSeenAt.Equal(now) || !stored.ExpiresAt.Equal(now.Add(24*time.Hour)) {
		t.Fatalf("Session 时间字段=%+v，期望创建/最后访问为当前时间且默认 TTL 为 24h", stored)
	}
	if stored.IPAddress != "127.0.0.1:43210" || len(stored.UserAgent) != 512 {
		t.Fatalf("Session 客户端信息=(%q, %d bytes)，期望保留 RemoteAddr 且 User-Agent 截断至 512 bytes", stored.IPAddress, len(stored.UserAgent))
	}
	if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), created.Token, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("使用原始令牌查询错误=%v，期望证明原始令牌未落库", err)
	}
	if created.Session.CSRFHash != csrfHash {
		t.Fatal("调用方 Session 必须携带 CSRF 哈希供双提交校验使用")
	}
	if _, exposed := reflect.TypeOf(Session{}).FieldByName("TokenHash"); exposed {
		t.Fatal("认证层公开 Session 不得暴露持久化 TokenHash")
	}
}

func TestSession认证统一拒绝空值畸形未知与过期令牌(t *testing.T) {
	opened := openAuthStore(t)
	now := time.Date(2026, 7, 27, 14, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	service := NewSessionService(opened, clock, bytes.NewReader(sequentialBytes(64)), time.Hour)
	adminID := createSessionTestAdmin(t, opened, now)
	created, err := service.Create(t.Context(), adminID, "127.0.0.1:43211", "session-test")
	if err != nil {
		t.Fatalf("准备 Session 失败: %v", err)
	}

	authenticated, err := service.Authenticate(t.Context(), created.Token)
	if err != nil || authenticated.ID != created.Session.ID || authenticated.AdminID != adminID {
		t.Fatalf("认证有效 Session 结果=(%+v, %v)", authenticated, err)
	}

	unknownToken := base64.RawURLEncoding.EncodeToString(bytes.Repeat([]byte{0xff}, 32))
	for name, token := range map[string]string{
		"空令牌":  "",
		"畸形令牌": "%%%not-base64url%%%",
		"未知令牌": unknownToken,
	} {
		t.Run(name, func(t *testing.T) {
			if _, err := service.Authenticate(t.Context(), token); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("认证错误=%v，期望 unauthorized", err)
			}
		})
	}

	clock.Set(created.Session.ExpiresAt)
	if _, err := service.Authenticate(t.Context(), created.Token); !errors.Is(err, ErrUnauthorized) {
		t.Fatalf("在 expires_at 边界认证错误=%v，期望 unauthorized", err)
	}
}

func TestSession退出撤销其他会话并清理过期记录(t *testing.T) {
	t.Run("退出幂等撤销当前会话", func(t *testing.T) {
		opened := openAuthStore(t)
		now := time.Date(2026, 7, 27, 15, 0, 0, 0, time.UTC)
		clock := newFakeClock(now)
		service := NewSessionService(opened, clock, bytes.NewReader(sequentialBytes(64)), time.Hour)
		adminID := createSessionTestAdmin(t, opened, now)
		created, err := service.Create(t.Context(), adminID, "127.0.0.1:43212", "logout-test")
		if err != nil {
			t.Fatalf("准备 Session 失败: %v", err)
		}

		if err := service.Logout(t.Context(), created.Session.ID); err != nil {
			t.Fatalf("退出失败: %v", err)
		}
		if _, err := service.Authenticate(t.Context(), created.Token); !errors.Is(err, ErrUnauthorized) {
			t.Fatalf("退出后认证错误=%v，期望 unauthorized", err)
		}
		if err := service.Logout(t.Context(), created.Session.ID); err != nil {
			t.Fatalf("重复退出应幂等成功，实际错误=%v", err)
		}
	})

	t.Run("只保留指定当前会话", func(t *testing.T) {
		opened := openAuthStore(t)
		now := time.Date(2026, 7, 27, 15, 30, 0, 0, time.UTC)
		clock := newFakeClock(now)
		service := NewSessionService(opened, clock, bytes.NewReader(sequentialBytes(64*3)), time.Hour)
		adminID := createSessionTestAdmin(t, opened, now)
		first, err := service.Create(t.Context(), adminID, "127.0.0.1:1", "first")
		if err != nil {
			t.Fatalf("创建第一个 Session 失败: %v", err)
		}
		current, err := service.Create(t.Context(), adminID, "127.0.0.1:2", "current")
		if err != nil {
			t.Fatalf("创建当前 Session 失败: %v", err)
		}
		third, err := service.Create(t.Context(), adminID, "127.0.0.1:3", "third")
		if err != nil {
			t.Fatalf("创建第三个 Session 失败: %v", err)
		}

		deleted, err := service.RevokeOthers(t.Context(), adminID, current.Session.ID)
		if err != nil || deleted != 2 {
			t.Fatalf("撤销其他 Session 结果=(%d, %v)，期望删除 2 条", deleted, err)
		}
		if _, err := service.Authenticate(t.Context(), current.Token); err != nil {
			t.Fatalf("当前 Session 应保留，认证错误=%v", err)
		}
		for _, revoked := range []CreatedSession{first, third} {
			if _, err := service.Authenticate(t.Context(), revoked.Token); !errors.Is(err, ErrUnauthorized) {
				t.Fatalf("被撤销 Session %s 的认证错误=%v", revoked.Session.ID, err)
			}
		}
	})

	t.Run("清理严格早于当前时间的记录", func(t *testing.T) {
		opened := openAuthStore(t)
		now := time.Date(2026, 7, 27, 16, 0, 0, 0, time.UTC)
		clock := newFakeClock(now)
		service := NewSessionService(opened, clock, bytes.NewReader(nil), time.Hour)
		adminID := createSessionTestAdmin(t, opened, now)
		expired := createStoredSession(t, opened, adminID, "expired-token-hash", now.Add(-time.Nanosecond))
		active := createStoredSession(t, opened, adminID, "active-token-hash", now.Add(time.Hour))

		deleted, err := service.CleanupExpired(t.Context())
		if err != nil || deleted != 1 {
			t.Fatalf("清理过期 Session 结果=(%d, %v)，期望删除 1 条", deleted, err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), expired.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("过期 Session 清理后查询错误=%v", err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), active.TokenHash, now); err != nil {
			t.Fatalf("有效 Session 不应被清理: %v", err)
		}
	})
}

func TestSession最后访问时间每五分钟至多写入一次(t *testing.T) {
	opened := openAuthStore(t)
	now := time.Date(2026, 7, 27, 17, 0, 0, 0, time.UTC)
	clock := newFakeClock(now)
	service := NewSessionService(opened, clock, bytes.NewReader(sequentialBytes(64)), time.Hour)
	adminID := createSessionTestAdmin(t, opened, now)
	created, err := service.Create(t.Context(), adminID, "127.0.0.1:43213", "touch-test")
	if err != nil {
		t.Fatalf("准备 Session 失败: %v", err)
	}
	tokenDigest := sha256.Sum256(mustDecodeToken(t, created.Token))
	tokenHash := hex.EncodeToString(tokenDigest[:])

	clock.Advance(4*time.Minute + 59*time.Second)
	untouched, err := service.Touch(t.Context(), created.Session)
	if err != nil {
		t.Fatalf("五分钟内 Touch 失败: %v", err)
	}
	if !untouched.LastSeenAt.Equal(now) {
		t.Fatalf("五分钟内 LastSeenAt=%v，期望保持 %v", untouched.LastSeenAt, now)
	}
	stored, err := opened.Sessions().GetActiveByTokenHash(t.Context(), tokenHash, clock.Now())
	if err != nil || !stored.LastSeenAt.Equal(now) {
		t.Fatalf("五分钟内持久化 LastSeenAt=%v，错误=%v", stored.LastSeenAt, err)
	}

	clock.Advance(time.Second)
	touched, err := service.Touch(t.Context(), untouched)
	if err != nil {
		t.Fatalf("五分钟边界 Touch 失败: %v", err)
	}
	if !touched.LastSeenAt.Equal(clock.Now()) {
		t.Fatalf("五分钟边界 LastSeenAt=%v，期望 %v", touched.LastSeenAt, clock.Now())
	}
	stored, err = opened.Sessions().GetActiveByTokenHash(t.Context(), tokenHash, clock.Now())
	if err != nil || !stored.LastSeenAt.Equal(clock.Now()) {
		t.Fatalf("五分钟边界持久化 LastSeenAt=%v，错误=%v", stored.LastSeenAt, err)
	}
}

type fakeClock struct {
	mu  sync.RWMutex
	now time.Time
}

func newFakeClock(now time.Time) *fakeClock {
	return &fakeClock{now: now.UTC()}
}

func (c *fakeClock) Now() time.Time {
	c.mu.RLock()
	defer c.mu.RUnlock()
	return c.now
}

func (c *fakeClock) Advance(delta time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(delta)
}

func (c *fakeClock) Set(now time.Time) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = now.UTC()
}

func sequentialBytes(length int) []byte {
	data := make([]byte, length)
	for index := range data {
		data[index] = byte(index + 1)
	}
	return data
}

func mustDecodeToken(t *testing.T, token string) []byte {
	t.Helper()
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("解码测试令牌失败: %v", err)
	}
	return decoded
}

func createSessionTestAdmin(t *testing.T, opened store.Store, now time.Time) string {
	t.Helper()
	adminID := ulid.Make().String()
	_, err := opened.Admins().Create(t.Context(), store.AdminAccount{
		ID:                adminID,
		Username:          "Session Admin",
		PasswordHash:      "session-test-password-hash",
		CreatedAt:         now,
		UpdatedAt:         now,
		PasswordChangedAt: now,
	})
	if err != nil {
		t.Fatalf("创建 Session 测试管理员失败: %v", err)
	}
	return adminID
}

func createStoredSession(
	t *testing.T,
	opened store.Store,
	adminID, tokenHash string,
	expiresAt time.Time,
) store.AdminSession {
	t.Helper()
	now := expiresAt.Add(-time.Hour)
	created, err := opened.Sessions().Create(t.Context(), store.AdminSession{
		ID:         ulid.Make().String(),
		AdminID:    adminID,
		TokenHash:  tokenHash,
		CSRFHash:   "csrf-" + tokenHash,
		CreatedAt:  now,
		ExpiresAt:  expiresAt,
		LastSeenAt: now,
		IPAddress:  "127.0.0.1:1",
		UserAgent:  "cleanup-test",
	})
	if err != nil {
		t.Fatalf("创建持久化 Session 失败: %v", err)
	}
	return created
}

package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	defaultSessionTTL    = 24 * time.Hour
	sessionTouchInterval = 5 * time.Minute
	maxUserAgentBytes    = 512
)

// Clock 为认证生命周期提供可测试的当前时间。
type Clock interface {
	Now() time.Time
}

// RandomReader 为 Session 与 CSRF 令牌提供可测试的安全随机源。
type RandomReader interface {
	Read([]byte) (int, error)
}

type systemClock struct{}

func (systemClock) Now() time.Time {
	return time.Now().UTC()
}

// Session 是认证与 HTTP 层使用的最小会话模型，不包含 Session 令牌哈希。
type Session struct {
	ID         string
	AdminID    string
	CSRFHash   string
	CreatedAt  time.Time
	ExpiresAt  time.Time
	LastSeenAt time.Time
	IPAddress  string
	UserAgent  string
}

// CreatedSession 仅在创建时携带一次性返回给客户端的原始令牌。
type CreatedSession struct {
	Session   Session
	Token     string
	CSRFToken string
}

// SessionService 管理只持久化哈希的管理员 Session 生命周期。
type SessionService struct {
	data   store.Store
	clock  Clock
	random RandomReader
	ttl    time.Duration
}

// NewSessionService 创建 Session 服务；非正 TTL 使用安全的 24 小时默认值。
func NewSessionService(
	data store.Store,
	clock Clock,
	random RandomReader,
	ttl time.Duration,
) *SessionService {
	if clock == nil {
		clock = systemClock{}
	}
	if random == nil {
		random = rand.Reader
	}
	if ttl <= 0 {
		ttl = defaultSessionTTL
	}
	return &SessionService{
		data:   data,
		clock:  clock,
		random: random,
		ttl:    ttl,
	}
}

// Create 创建 Session，并且只在本次返回原始 Session 与 CSRF 令牌。
func (s *SessionService) Create(
	ctx context.Context,
	adminID, ipAddress, userAgent string,
) (CreatedSession, error) {
	adminID = strings.TrimSpace(adminID)
	if adminID == "" {
		return CreatedSession{}, ErrValidationFailed
	}
	token, tokenHash, err := issueRandomToken(s.random)
	if err != nil {
		return CreatedSession{}, err
	}
	csrfToken, csrfHash, err := issueRandomToken(s.random)
	if err != nil {
		return CreatedSession{}, err
	}

	now := s.clock.Now().UTC()
	created, err := s.data.Sessions().Create(ctx, store.AdminSession{
		ID:         ulid.Make().String(),
		AdminID:    adminID,
		TokenHash:  tokenHash,
		CSRFHash:   csrfHash,
		CreatedAt:  now,
		ExpiresAt:  now.Add(s.ttl),
		LastSeenAt: now,
		IPAddress:  strings.TrimSpace(ipAddress),
		UserAgent:  truncateUTF8Bytes(userAgent, maxUserAgentBytes),
	})
	if err != nil {
		return CreatedSession{}, err
	}
	return CreatedSession{
		Session:   publicSession(created),
		Token:     token,
		CSRFToken: csrfToken,
	}, nil
}

// Authenticate 验证原始令牌并统一隐藏无效、未知与过期状态。
func (s *SessionService) Authenticate(ctx context.Context, rawToken string) (Session, error) {
	tokenHash, err := hashEncodedToken(rawToken)
	if err != nil {
		return Session{}, ErrUnauthorized
	}
	active, err := s.data.Sessions().GetActiveByTokenHash(ctx, tokenHash, s.clock.Now().UTC())
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	return publicSession(active), nil
}

// Touch 最多每五分钟更新一次最后访问时间。
func (s *SessionService) Touch(ctx context.Context, session Session) (Session, error) {
	now := s.clock.Now().UTC()
	if strings.TrimSpace(session.ID) == "" || !now.Before(session.ExpiresAt) {
		return Session{}, ErrUnauthorized
	}
	if !session.LastSeenAt.IsZero() && now.Sub(session.LastSeenAt) < sessionTouchInterval {
		return session, nil
	}
	touched, err := s.data.Sessions().Touch(ctx, session.ID, now)
	if errors.Is(err, store.ErrNotFound) {
		return Session{}, ErrUnauthorized
	}
	if err != nil {
		return Session{}, err
	}
	return publicSession(touched), nil
}

// Logout 幂等撤销当前 Session。
func (s *SessionService) Logout(ctx context.Context, sessionID string) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil
	}
	err := s.data.Sessions().Revoke(ctx, sessionID)
	if errors.Is(err, store.ErrNotFound) {
		return nil
	}
	return err
}

// RevokeOthers 撤销管理员除当前 Session 外的所有会话。
func (s *SessionService) RevokeOthers(
	ctx context.Context,
	adminID, keepSessionID string,
) (int, error) {
	adminID = strings.TrimSpace(adminID)
	keepSessionID = strings.TrimSpace(keepSessionID)
	if adminID == "" || keepSessionID == "" {
		return 0, ErrValidationFailed
	}
	return s.data.Sessions().DeleteOthers(ctx, adminID, keepSessionID)
}

// CleanupExpired 删除当前时间点已失效的 Session。
func (s *SessionService) CleanupExpired(ctx context.Context) (int, error) {
	return s.data.Sessions().CleanupExpired(ctx, s.clock.Now().UTC())
}

func publicSession(session store.AdminSession) Session {
	return Session{
		ID:         session.ID,
		AdminID:    session.AdminID,
		CSRFHash:   session.CSRFHash,
		CreatedAt:  session.CreatedAt.UTC(),
		ExpiresAt:  session.ExpiresAt.UTC(),
		LastSeenAt: session.LastSeenAt.UTC(),
		IPAddress:  session.IPAddress,
		UserAgent:  session.UserAgent,
	}
}

func truncateUTF8Bytes(value string, limit int) string {
	if limit <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "")
	if len(value) <= limit {
		return value
	}
	truncated := value[:limit]
	for !utf8.ValidString(truncated) {
		truncated = truncated[:len(truncated)-1]
	}
	return truncated
}

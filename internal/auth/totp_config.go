package auth

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	// TOTPSettingKey 是 system_settings 表中 TOTP 二步验证的配置项键名。
	TOTPSettingKey        = "security.totp_config"
	totpSecretAAD         = "reposentinel:totp-secret:v1"
	maxTOTPTicketFailures = 3
	maxActiveTickets      = 2000
)

// StoredTOTPConfig 持久化在数据库中的 2FA 状态与信封加密密钥。
type StoredTOTPConfig struct {
	Enabled        bool      `json:"enabled"`
	SecretEnvelope string    `json:"secret_envelope,omitempty"`
	PlainSecret    string    `json:"plain_secret,omitempty"`
	UpdatedAt      time.Time `json:"updated_at"`
}

// LoadTOTPConfig 从数据库读取 2FA 配置并解密 TOTP 密钥。
func LoadTOTPConfig(ctx context.Context, data store.Store, ring *cryptox.KeyRing) (bool, string, error) {
	if data == nil {
		return false, "", nil
	}
	setting, err := data.Settings().Get(ctx, TOTPSettingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return false, "", nil
		}
		return false, "", err
	}

	var stored StoredTOTPConfig
	if err := json.Unmarshal(setting.ValueJSON, &stored); err != nil {
		return false, "", fmt.Errorf("%w: decode stored config: %v", ErrInvalidTOTPConfig, err)
	}
	if !stored.Enabled {
		return false, "", nil
	}

	if stored.SecretEnvelope != "" {
		if ring == nil {
			return false, "", fmt.Errorf("%w: key ring unavailable", ErrInvalidTOTPConfig)
		}
		decrypted, err := ring.Decrypt(ctx, stored.SecretEnvelope, []byte(totpSecretAAD))
		if err != nil {
			return false, "", fmt.Errorf("%w: decrypt secret: %v", ErrInvalidTOTPConfig, err)
		}
		if strings.TrimSpace(string(decrypted.Plaintext)) == "" {
			return false, "", fmt.Errorf("%w: empty decrypted secret", ErrInvalidTOTPConfig)
		}
		return true, string(decrypted.Plaintext), nil
	}

	if stored.PlainSecret != "" {
		return true, stored.PlainSecret, nil
	}

	return false, "", fmt.Errorf("%w: enabled config has no secret", ErrInvalidTOTPConfig)
}

// SaveTOTPConfig 加密 TOTP 密钥并保存到数据库。
func SaveTOTPConfig(ctx context.Context, data store.Store, ring *cryptox.KeyRing, enabled bool, secret string) error {
	if data == nil {
		return errors.New("store is not available")
	}

	stored := StoredTOTPConfig{
		Enabled:   enabled,
		UpdatedAt: time.Now().UTC(),
	}

	cleanSecret := strings.TrimSpace(secret)
	if enabled && cleanSecret != "" {
		if ring != nil {
			envelope, err := ring.Encrypt(ctx, []byte(cleanSecret), []byte(totpSecretAAD))
			if err != nil {
				return err
			}
			stored.SecretEnvelope = envelope
		} else {
			stored.PlainSecret = cleanSecret
		}
	}

	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}

	_, err = data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       TOTPSettingKey,
		ValueJSON: raw,
		UpdatedAt: stored.UpdatedAt,
		UpdatedBy: "admin",
	})
	return err
}

// DisableTOTP 禁用 2FA 并清除密钥。
func DisableTOTP(ctx context.Context, data store.Store) error {
	return SaveTOTPConfig(ctx, data, nil, false, "")
}

// TOTPTicket 记录登录两阶段之间的临时认证票据。
type TOTPTicket struct {
	AdminID   string
	Username  string
	RemoteIP  string
	Failures  int
	ExpiresAt time.Time
}

// TOTPTicketManager 管理内存中的两阶段临时凭据。
type TOTPTicketManager struct {
	mu      sync.Mutex
	ttl     time.Duration
	tickets map[string]*TOTPTicket
}

// NewTOTPTicketManager 创建两阶段票据管理器。
func NewTOTPTicketManager(ttl time.Duration) *TOTPTicketManager {
	if ttl <= 0 {
		ttl = 3 * time.Minute
	}
	return &TOTPTicketManager{
		ttl:     ttl,
		tickets: make(map[string]*TOTPTicket),
	}
}

// CreateTicket 创建一个新的临时票据并返回票据 ID。
// 为保证安全性，同一管理员只允许保留一个最新的活跃票据，旧票据将被自动作废。
func (m *TOTPTicketManager) CreateTicket(adminID, username, remoteIP string) string {
	ticketID := ulid.Make().String()
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	m.prune(now)

	// 1. 作废该管理员名下所有现存旧票据
	cleanAdminID := strings.TrimSpace(adminID)
	if cleanAdminID != "" {
		for id, ticket := range m.tickets {
			if ticket.AdminID == cleanAdminID {
				delete(m.tickets, id)
			}
		}
	}

	// 2. 容量保护：若超出上限，强制清理
	if len(m.tickets) >= maxActiveTickets {
		for id := range m.tickets {
			delete(m.tickets, id)
			if len(m.tickets) < maxActiveTickets {
				break
			}
		}
	}

	m.tickets[ticketID] = &TOTPTicket{
		AdminID:   adminID,
		Username:  username,
		RemoteIP:  strings.TrimSpace(remoteIP),
		ExpiresAt: now.Add(m.ttl),
	}
	return ticketID
}

// GetTicket 校验并读取票据信息。
func (m *TOTPTicketManager) GetTicket(ticketID, remoteIP string) (*TOTPTicket, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()

	now := time.Now().UTC()
	ticket, ok := m.tickets[ticketID]
	if !ok || ticket == nil {
		return nil, false
	}
	if now.After(ticket.ExpiresAt) {
		delete(m.tickets, ticketID)
		return nil, false
	}
	if strings.TrimSpace(remoteIP) != "" && ticket.RemoteIP != "" && ticket.RemoteIP != strings.TrimSpace(remoteIP) {
		return nil, false
	}
	return ticket, true
}

// RecordFailure 记录一次动态码校验失败，返回当前失败次数。如果失败达到上限，销毁票据。
func (m *TOTPTicketManager) RecordFailure(ticketID string) int {
	m.mu.Lock()
	defer m.mu.Unlock()

	ticket, ok := m.tickets[ticketID]
	if !ok || ticket == nil {
		return maxTOTPTicketFailures
	}
	ticket.Failures++
	if ticket.Failures >= maxTOTPTicketFailures {
		delete(m.tickets, ticketID)
		return maxTOTPTicketFailures
	}
	return ticket.Failures
}

// ConsumeTicket 单次成功校验后立刻销毁票据。
func (m *TOTPTicketManager) ConsumeTicket(ticketID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	if _, ok := m.tickets[ticketID]; !ok {
		return false
	}
	delete(m.tickets, ticketID)
	return true
}

func (m *TOTPTicketManager) prune(now time.Time) {
	for id, ticket := range m.tickets {
		if now.After(ticket.ExpiresAt) {
			delete(m.tickets, id)
		}
	}
}

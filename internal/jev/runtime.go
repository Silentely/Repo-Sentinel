package jev

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	// RuntimeSettingKey 是 system_settings 中 Jev 可编辑配置的键。
	RuntimeSettingKey = "jev.runtime.v1"
	// SecretAAD 是 Jev API Key 加密信封的附加认证数据。
	SecretAAD = "reposentinel:jev_api_key"
)

// StoredConfig 是数据库中持久化的 Jev 配置（API Key 为密文信封）。
type StoredConfig struct {
	Enabled                 *bool               `json:"enabled,omitempty"`
	BaseURL                 string              `json:"base_url,omitempty"`
	Model                   string              `json:"model,omitempty"`
	Mode                    string              `json:"mode,omitempty"`
	Retries                 *int                `json:"retries,omitempty"`
	APIKeyEnvelope          string              `json:"api_key_envelope,omitempty"`
	NoiseFilterEnabled      *bool               `json:"noise_filter_enabled,omitempty"`
	RoutingEnabled          *bool               `json:"routing_enabled,omitempty"`
	CascadeGateEnabled      *bool               `json:"cascade_gate_enabled,omitempty"`
	ChatOpsGuardrailEnabled *bool               `json:"chatops_guardrail_enabled,omitempty"`
	AllowPrivateState       *bool               `json:"allow_private_state,omitempty"`
	Notification            *config.PlaneConfig `json:"notification,omitempty"`
	Review                  *config.PlaneConfig `json:"review,omitempty"`
	ChatOps                 *config.PlaneConfig `json:"chatops,omitempty"`

	EmergencyAdvisoryUntil  *time.Time `json:"emergency_advisory_until,omitempty"`
	EmergencyAdvisoryReason string     `json:"emergency_advisory_reason,omitempty"`
	ShadowStartedAt         *time.Time `json:"shadow_started_at,omitempty"`
}

// RuntimeConfig 持有进程内可热更新的 Jev 运行时配置与各决策面客户端。
type RuntimeConfig struct {
	mu sync.RWMutex

	cfg                  config.JevConfig
	clients              map[string]Client
	generation           uint64
	credentialGeneration uint64
	envApiKeySet         bool

	emergencyAdvisoryUntil time.Time
	emergencyReason        string
}

// RuntimeFromEnv 从静态环境配置构建初始运行时。
func RuntimeFromEnv(cfg config.JevConfig) *RuntimeConfig {
	rt := &RuntimeConfig{
		cfg:                  cfg,
		clients:              make(map[string]Client),
		generation:           1,
		credentialGeneration: 1,
		envApiKeySet:         cfg.APIKey.Reveal() != "",
	}
	rt.rebuildClientsLocked()
	return rt
}

func (r *RuntimeConfig) rebuildClientsLocked() {
	if !r.cfg.Enabled {
		r.clients = make(map[string]Client)
		return
	}

	planes := []struct {
		name string
		conf config.PlaneConfig
	}{
		{"notification", r.cfg.Notification},
		{"review", r.cfg.Review},
		{"chatops", r.cfg.ChatOps},
	}

	for _, p := range planes {
		timeout := p.conf.Timeout
		if timeout <= 0 || timeout > 60*time.Second {
			timeout = 30 * time.Second
		}
		opts := ClientOptions{
			BaseURL:              r.cfg.BaseURL,
			APIKey:               r.cfg.APIKey,
			Model:                r.cfg.Model,
			Plane:                p.name,
			PolicyVersion:        "jev-policy-v1",
			CredentialGeneration: r.credentialGeneration,
			RuntimeGeneration:    r.generation,
			Timeout:              timeout,
			MaxConcurrency:       p.conf.MaxConcurrency,
			Retries:              r.cfg.Retries,
		}
		cli, err := NewClient(opts)
		if err == nil {
			r.clients[p.name] = cli
		}
	}
}

// Snapshot 获取当前 Jev 运行时配置的深拷贝快照。
func (r *RuntimeConfig) Snapshot() config.JevConfig {
	r.mu.RLock()
	defer r.mu.RUnlock()

	cp := r.cfg
	cp.APIKey = config.NewSecret(r.cfg.APIKey.Reveal())
	return cp
}

// Client 获取特定决策面的生效 Client 实例。
func (r *RuntimeConfig) Client(plane string) Client {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.clients[plane]
}

// SetClientForTest 注入指定决策面的测试客户端（仅供单元测试使用）。
func (r *RuntimeConfig) SetClientForTest(plane string, cli Client) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.clients == nil {
		r.clients = make(map[string]Client)
	}
	r.clients[plane] = cli
}

// Replace 原子替换整个运行时配置。
func (r *RuntimeConfig) Replace(next *RuntimeConfig) {
	if next == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()

	next.mu.RLock()
	defer next.mu.RUnlock()

	r.cfg = next.cfg
	r.clients = next.clients
	r.generation++
	r.credentialGeneration = next.credentialGeneration
	r.emergencyAdvisoryUntil = next.emergencyAdvisoryUntil
	r.emergencyReason = next.emergencyReason
}

// SetEmergencyAdvisory 显式设置运行时的应急旁路降级截止时间与原因。
func (r *RuntimeConfig) SetEmergencyAdvisory(until time.Time, reason string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.emergencyAdvisoryUntil = until
	r.emergencyReason = reason
}

// EmergencyAdvisory 获取当前运行时的应急降级状态。
func (r *RuntimeConfig) EmergencyAdvisory() (time.Time, string) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.emergencyAdvisoryUntil, r.emergencyReason
}

// IsChatOpsAdvisory 检查当前 ChatOps 是否处于 emergency advisory 或普通 advisory 模式。
func (r *RuntimeConfig) IsChatOpsAdvisory(now time.Time) bool {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if r.cfg.Mode == "shadow" {
		return true
	}
	if r.cfg.Mode == "active" {
		return !r.emergencyAdvisoryUntil.IsZero() && now.Before(r.emergencyAdvisoryUntil)
	}
	return false
}

// EncryptAPIKey 加密 Jev API Key。
func EncryptAPIKey(ctx context.Context, ring *cryptox.KeyRing, plain string) (string, error) {
	if ring == nil {
		return "", errors.New("jev: keyring is required")
	}
	if plain == "" {
		return "", nil
	}
	return ring.Encrypt(ctx, []byte(plain), []byte(SecretAAD))
}

// DecryptAPIKey 解密 Jev API Key 信封。
func DecryptAPIKey(ctx context.Context, ring *cryptox.KeyRing, envelope string) (string, error) {
	if ring == nil {
		return "", errors.New("jev: keyring is required")
	}
	if envelope == "" {
		return "", nil
	}
	res, err := ring.Decrypt(ctx, envelope, []byte(SecretAAD))
	if err != nil {
		return "", err
	}
	return string(res.Plaintext), nil
}

// LoadStoredConfig 从持久化存储读取配置。
func LoadStoredConfig(ctx context.Context, data store.Store) (StoredConfig, error) {
	var stored StoredConfig
	if data == nil {
		return stored, nil
	}
	setting, err := data.Settings().Get(ctx, RuntimeSettingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return stored, nil
		}
		return stored, err
	}
	if err := json.Unmarshal(setting.ValueJSON, &stored); err != nil {
		return StoredConfig{}, fmt.Errorf("parse jev runtime config: %w", err)
	}
	return stored, nil
}

// SaveStoredConfig 将配置写入持久化存储。
func SaveStoredConfig(ctx context.Context, data store.Store, stored StoredConfig) error {
	raw, err := json.Marshal(stored)
	if err != nil {
		return err
	}
	_, err = data.Settings().Upsert(ctx, store.SystemSetting{
		ID:        ulid.Make().String(),
		Key:       RuntimeSettingKey,
		ValueJSON: raw,
		UpdatedAt: time.Now().UTC(),
		UpdatedBy: "admin",
	})
	return err
}

// MergeFromStore 从持久化存储读取配置并与当前环境变量运行时合并。
func MergeFromStore(ctx context.Context, data store.Store, ring *cryptox.KeyRing, rt *RuntimeConfig) error {
	if data == nil || rt == nil {
		return nil
	}

	setting, err := data.Settings().Get(ctx, RuntimeSettingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return err
	}

	var stored StoredConfig
	if err := json.Unmarshal(setting.ValueJSON, &stored); err != nil {
		return fmt.Errorf("jev: invalid stored config json: %w", err)
	}

	rt.mu.Lock()
	defer rt.mu.Unlock()

	// 环境变量优先；未设置的字段叠加数据库值
	if stored.Enabled != nil {
		rt.cfg.Enabled = *stored.Enabled
	}
	if stored.BaseURL != "" {
		rt.cfg.BaseURL = stored.BaseURL
	}
	if stored.Model != "" {
		rt.cfg.Model = stored.Model
	}
	if stored.Mode != "" {
		rt.cfg.Mode = stored.Mode
	}
	if stored.Retries != nil {
		rt.cfg.Retries = *stored.Retries
	}
	if stored.NoiseFilterEnabled != nil {
		rt.cfg.NoiseFilterEnabled = *stored.NoiseFilterEnabled
	}
	if stored.RoutingEnabled != nil {
		rt.cfg.RoutingEnabled = *stored.RoutingEnabled
	}
	if stored.CascadeGateEnabled != nil {
		rt.cfg.CascadeGateEnabled = *stored.CascadeGateEnabled
	}
	if stored.ChatOpsGuardrailEnabled != nil {
		rt.cfg.ChatOpsGuardrailEnabled = *stored.ChatOpsGuardrailEnabled
	}
	if stored.AllowPrivateState != nil {
		rt.cfg.AllowPrivateState = *stored.AllowPrivateState
	}
	if stored.Notification != nil {
		rt.cfg.Notification = *stored.Notification
		if rt.cfg.Notification.Timeout <= 1000*time.Millisecond {
			rt.cfg.Notification.Timeout = 30 * time.Second
		}
	}
	if stored.Review != nil {
		rt.cfg.Review = *stored.Review
		if rt.cfg.Review.Timeout <= 1000*time.Millisecond {
			rt.cfg.Review.Timeout = 30 * time.Second
		}
	}
	if stored.ChatOps != nil {
		rt.cfg.ChatOps = *stored.ChatOps
		if rt.cfg.ChatOps.Timeout <= 1000*time.Millisecond {
			rt.cfg.ChatOps.Timeout = 30 * time.Second
		}
	}

	if stored.EmergencyAdvisoryUntil != nil {
		rt.emergencyAdvisoryUntil = *stored.EmergencyAdvisoryUntil
		rt.emergencyReason = stored.EmergencyAdvisoryReason
	}

	if !rt.envApiKeySet && stored.APIKeyEnvelope != "" && ring != nil {
		plain, decErr := DecryptAPIKey(ctx, ring, stored.APIKeyEnvelope)
		if decErr != nil {
			return fmt.Errorf("jev: decrypt api key envelope failed: %w", decErr)
		}
		rt.cfg.APIKey = config.NewSecret(plain)
		rt.credentialGeneration++
	}

	rt.generation++
	rt.rebuildClientsLocked()
	return nil
}

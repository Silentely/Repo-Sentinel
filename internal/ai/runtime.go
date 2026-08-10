package ai

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	// RuntimeSettingKey 是 system_settings 中 AI 可编辑配置的键。
	RuntimeSettingKey = "ai.runtime_config"
	// secretAAD 是 AI 配置密钥信封的附加认证数据。
	secretAAD = "reposentinel:ai-runtime:v1"
)

// StoredConfig 是数据库中的可编辑 AI 配置（API Key 为密钥信封）。
// bool/int 字段用指针区分「未设置」与「显式值」，支持 env 优先、DB 补缺语义。
type StoredConfig struct {
	Enabled        *bool  `json:"enabled,omitempty"`
	BaseURL        string `json:"base_url,omitempty"`
	Model          string `json:"model,omitempty"`
	TimeoutSec     *int64 `json:"timeout_sec,omitempty"`
	MaxTokens      *int   `json:"max_tokens,omitempty"`
	Retries        *int   `json:"retries,omitempty"`
	APIKeyEnvelope string `json:"api_key_envelope,omitempty"`
	DigestEnabled  *bool  `json:"digest_enabled,omitempty"`
	TriageEnabled  *bool  `json:"triage_enabled,omitempty"`
}

// RuntimeConfig 持有进程内可热更新的 AI 配置。
// 环境变量在启动时写入后，管理台可在 env 未设置的字段上叠加数据库值。
type RuntimeConfig struct {
	mu sync.RWMutex

	Enabled       bool
	BaseURL       string
	Model         string
	Timeout       time.Duration
	MaxTokens     int
	Retries       int
	APIKey        string
	DigestEnabled bool
	TriageEnabled bool

	// 字段来源：env | database | unset（仅状态展示，不回显密钥）。
	EnabledSource       string
	BaseURLSource       string
	ModelSource         string
	TimeoutSource       string
	MaxTokensSource     string
	RetriesSource       string
	APIKeySource        string
	DigestEnabledSource string
	TriageEnabledSource string
}

// RuntimeFromEnv 从环境变量配置构建运行时基线并标记来源。
// bool 开关以「偏离默认值」判定显式设置（enabled 默认 false、digest/triage 默认 true），
// 避免把默认值误判为 env 锁定导致管理台无法覆盖；Retries 同为「偏离默认（1）」判定。
func RuntimeFromEnv(cfg config.AIConfig) *RuntimeConfig {
	return &RuntimeConfig{
		Enabled:             cfg.Enabled,
		BaseURL:             strings.TrimSpace(cfg.BaseURL),
		Model:               strings.TrimSpace(cfg.Model),
		Timeout:             cfg.Timeout,
		MaxTokens:           cfg.MaxTokens,
		Retries:             cfg.Retries,
		APIKey:              cfg.APIKey.Reveal(),
		DigestEnabled:       cfg.DigestEnabled,
		TriageEnabled:       cfg.TriageEnabled,
		EnabledSource:       sourceLabel(cfg.Enabled, "env"),
		BaseURLSource:       sourceLabel(strings.TrimSpace(cfg.BaseURL) != "", "env"),
		ModelSource:         sourceLabel(strings.TrimSpace(cfg.Model) != "", "env"),
		TimeoutSource:       sourceLabel(cfg.Timeout > 0, "env"),
		MaxTokensSource:     sourceLabel(cfg.MaxTokens > 0, "env"),
		RetriesSource:       sourceLabel(cfg.Retries != DefaultRetries, "env"),
		APIKeySource:        sourceLabel(cfg.APIKey.Reveal() != "", "env"),
		DigestEnabledSource: sourceLabel(!cfg.DigestEnabled, "env"),
		TriageEnabledSource: sourceLabel(!cfg.TriageEnabled, "env"),
	}
}

// Snapshot 返回当前只读副本。
func (r *RuntimeConfig) Snapshot() RuntimeConfig {
	if r == nil {
		return RuntimeConfig{}
	}
	r.mu.RLock()
	defer r.mu.RUnlock()
	return RuntimeConfig{
		Enabled:             r.Enabled,
		BaseURL:             r.BaseURL,
		Model:               r.Model,
		Timeout:             r.Timeout,
		MaxTokens:           r.MaxTokens,
		Retries:             r.Retries,
		APIKey:              r.APIKey,
		DigestEnabled:       r.DigestEnabled,
		TriageEnabled:       r.TriageEnabled,
		EnabledSource:       r.EnabledSource,
		BaseURLSource:       r.BaseURLSource,
		ModelSource:         r.ModelSource,
		TimeoutSource:       r.TimeoutSource,
		MaxTokensSource:     r.MaxTokensSource,
		RetriesSource:       r.RetriesSource,
		APIKeySource:        r.APIKeySource,
		DigestEnabledSource: r.DigestEnabledSource,
		TriageEnabledSource: r.TriageEnabledSource,
	}
}

// Replace 用完整快照替换可变字段。
func (r *RuntimeConfig) Replace(next *RuntimeConfig) {
	if r == nil || next == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	r.Enabled = next.Enabled
	r.BaseURL = next.BaseURL
	r.Model = next.Model
	r.Timeout = next.Timeout
	r.MaxTokens = next.MaxTokens
	r.Retries = next.Retries
	r.APIKey = next.APIKey
	r.DigestEnabled = next.DigestEnabled
	r.TriageEnabled = next.TriageEnabled
	r.EnabledSource = next.EnabledSource
	r.BaseURLSource = next.BaseURLSource
	r.ModelSource = next.ModelSource
	r.TimeoutSource = next.TimeoutSource
	r.MaxTokensSource = next.MaxTokensSource
	r.RetriesSource = next.RetriesSource
	r.APIKeySource = next.APIKeySource
	r.DigestEnabledSource = next.DigestEnabledSource
	r.TriageEnabledSource = next.TriageEnabledSource
}

// Client 将当前运行时配置物化为可用的 AI 客户端。
func (r *RuntimeConfig) Client() *Client {
	snap := r.Snapshot()
	return &Client{
		Enabled:       snap.Enabled,
		BaseURL:       snap.BaseURL,
		APIKey:        snap.APIKey,
		Model:         snap.Model,
		Timeout:       snap.Timeout,
		MaxTokens:     snap.MaxTokens,
		Retries:       snap.Retries,
		DigestEnabled: snap.DigestEnabled,
		TriageEnabled: snap.TriageEnabled,
	}
}

// LoadStoredConfig 读取数据库中的 AI 配置；不存在时返回零值。
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
	_ = json.Unmarshal(setting.ValueJSON, &stored)
	return stored, nil
}

// SaveStoredConfig 写入数据库。
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

// EncryptAPIKey 加密 AI API Key。
func EncryptAPIKey(ctx context.Context, keyRing *cryptox.KeyRing, plain string) (string, error) {
	if keyRing == nil {
		return "", cryptox.ErrEncryptionKeyMismatch
	}
	return keyRing.Encrypt(ctx, []byte(plain), []byte(secretAAD))
}

// DecryptAPIKey 解密 AI API Key。
func DecryptAPIKey(ctx context.Context, keyRing *cryptox.KeyRing, envelope string) (string, error) {
	if keyRing == nil {
		return "", cryptox.ErrEncryptionKeyMismatch
	}
	res, err := keyRing.Decrypt(ctx, envelope, []byte(secretAAD))
	if err != nil {
		return "", err
	}
	return string(res.Plaintext), nil
}

// MergeFromStore 用数据库值填充 RuntimeConfig 中「仍为 env 未设置」的字段。
// 与 GitHub 运行时配置同语义：env 优先，DB 补缺。
func MergeFromStore(ctx context.Context, data store.Store, keyRing *cryptox.KeyRing, rt *RuntimeConfig) error {
	if data == nil || rt == nil {
		return nil
	}
	stored, err := LoadStoredConfig(ctx, data)
	if err != nil {
		return err
	}
	if stored == (StoredConfig{}) {
		return nil
	}

	snap := rt.Snapshot()

	if snap.EnabledSource != "env" && stored.Enabled != nil {
		snap.Enabled = *stored.Enabled
		snap.EnabledSource = "database"
	}
	if strings.TrimSpace(snap.BaseURL) == "" && strings.TrimSpace(stored.BaseURL) != "" {
		snap.BaseURL = strings.TrimSpace(stored.BaseURL)
		snap.BaseURLSource = "database"
	}
	if strings.TrimSpace(snap.Model) == "" && strings.TrimSpace(stored.Model) != "" {
		snap.Model = strings.TrimSpace(stored.Model)
		snap.ModelSource = "database"
	}
	if snap.Timeout <= 0 && stored.TimeoutSec != nil && *stored.TimeoutSec > 0 {
		snap.Timeout = time.Duration(*stored.TimeoutSec) * time.Second
		snap.TimeoutSource = "database"
	}
	if snap.MaxTokens <= 0 && stored.MaxTokens != nil && *stored.MaxTokens > 0 {
		snap.MaxTokens = *stored.MaxTokens
		snap.MaxTokensSource = "database"
	}
	if snap.RetriesSource != "env" && stored.Retries != nil {
		snap.Retries = *stored.Retries
		snap.RetriesSource = "database"
	}
	if strings.TrimSpace(snap.APIKey) == "" && strings.TrimSpace(stored.APIKeyEnvelope) != "" && keyRing != nil {
		if plain, err := DecryptAPIKey(ctx, keyRing, stored.APIKeyEnvelope); err == nil && plain != "" {
			snap.APIKey = plain
			snap.APIKeySource = "database"
		}
	}
	if snap.DigestEnabledSource != "env" && stored.DigestEnabled != nil {
		snap.DigestEnabled = *stored.DigestEnabled
		snap.DigestEnabledSource = "database"
	}
	if snap.TriageEnabledSource != "env" && stored.TriageEnabled != nil {
		snap.TriageEnabled = *stored.TriageEnabled
		snap.TriageEnabledSource = "database"
	}

	rt.Replace(&snap)
	return nil
}

// sourceLabel 标记字段来源：有值记为 source，否则 unset。
func sourceLabel(ok bool, source string) string {
	if ok {
		return source
	}
	return "unset"
}

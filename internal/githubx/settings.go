package githubx

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	// RuntimeSettingKey 是 system_settings 中 GitHub 可编辑配置的键。
	RuntimeSettingKey = "github.runtime_config"
	secretAAD         = "reposentinel:github-runtime:v1"
)

// StoredRuntime 是数据库中的可编辑 GitHub 配置（密钥为信封）。
type StoredRuntime struct {
	AppID                 int64  `json:"app_id,omitempty"`
	ClientID              string `json:"client_id,omitempty"`
	PrivateKeyPath        string `json:"private_key_path,omitempty"`
	PrivateKeyPEMEnvelope string `json:"private_key_pem_envelope,omitempty"`
	WebhookSecretEnvelope string `json:"webhook_secret_envelope,omitempty"`
	PublicBaseURL         string `json:"public_base_url,omitempty"`
}

// LoadStoredRuntime 读取数据库中的 GitHub 配置；不存在时返回零值。
func LoadStoredRuntime(ctx context.Context, data store.Store) (StoredRuntime, error) {
	var stored StoredRuntime
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

// SaveStoredRuntime 写入数据库。
func SaveStoredRuntime(ctx context.Context, data store.Store, stored StoredRuntime) error {
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

// EncryptSecret 加密 GitHub 相关敏感字段。
func EncryptSecret(ctx context.Context, keyRing *cryptox.KeyRing, plain string) (string, error) {
	if keyRing == nil {
		return "", cryptox.ErrEncryptionKeyMismatch
	}
	return keyRing.Encrypt(ctx, []byte(plain), []byte(secretAAD))
}

// DecryptSecret 解密 GitHub 相关敏感字段。
func DecryptSecret(ctx context.Context, keyRing *cryptox.KeyRing, envelope string) (string, error) {
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
func MergeFromStore(ctx context.Context, data store.Store, keyRing *cryptox.KeyRing, rt *RuntimeConfig) error {
	if data == nil || rt == nil {
		return nil
	}
	stored, err := LoadStoredRuntime(ctx, data)
	if err != nil {
		return err
	}
	if stored == (StoredRuntime{}) {
		return nil
	}

	snap := rt.Snapshot()
	snap.Client = rt.Client

	if snap.AppID <= 0 && stored.AppID > 0 {
		snap.AppID = stored.AppID
		snap.AppIDSource = "database"
	}
	if strings.TrimSpace(snap.ClientID) == "" && strings.TrimSpace(stored.ClientID) != "" {
		snap.ClientID = strings.TrimSpace(stored.ClientID)
		snap.ClientIDSource = "database"
	}
	if strings.TrimSpace(snap.PrivateKeyPath) == "" && strings.TrimSpace(snap.PrivateKeyPEM) == "" {
		if path := strings.TrimSpace(stored.PrivateKeyPath); path != "" {
			snap.PrivateKeyPath = path
			snap.PrivateKeySource = "database"
		}
		if env := strings.TrimSpace(stored.PrivateKeyPEMEnvelope); env != "" && keyRing != nil {
			if plain, err := DecryptSecret(ctx, keyRing, env); err == nil && plain != "" {
				snap.PrivateKeyPEM = plain
				snap.PrivateKeyPath = ""
				snap.PrivateKeySource = "database"
			}
		}
	}
	if strings.TrimSpace(snap.WebhookSecret) == "" && strings.TrimSpace(stored.WebhookSecretEnvelope) != "" && keyRing != nil {
		if plain, err := DecryptSecret(ctx, keyRing, stored.WebhookSecretEnvelope); err == nil && plain != "" {
			snap.WebhookSecret = plain
			snap.WebhookSecretSource = "database"
		}
	}
	if strings.TrimSpace(snap.PublicBaseURL) == "" && strings.TrimSpace(stored.PublicBaseURL) != "" {
		snap.PublicBaseURL = strings.TrimSpace(stored.PublicBaseURL)
		snap.PublicBaseURLSource = "database"
	}

	rt.Replace(&snap)
	return nil
}

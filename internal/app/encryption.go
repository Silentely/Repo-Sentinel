package app

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	encryptionProbeSettingKey = "security.encryption_probe"
	encryptionProbePlaintext  = "reposentinel-key-check-v1"
	encryptionProbeAAD        = "reposentinel:encryption-probe:v1"
)

type encryptionProbe struct {
	Envelope string `json:"envelope"`
}

// validateEncryptionKey 校验当前配置的主密钥是否能解密数据库中的探针；
// 若探针不存在则视为首次启动，写入新探针后返回密钥环。
func validateEncryptionKey(
	ctx context.Context,
	data store.Store,
	cfg config.EncryptionConfig,
) (*cryptox.KeyRing, error) {
	setting, err := data.Settings().Get(ctx, encryptionProbeSettingKey)
	probeExists := err == nil
	if err != nil && !errors.Is(err, store.ErrNotFound) {
		return nil, err
	}
	if strings.TrimSpace(cfg.CurrentKey.Reveal()) == "" {
		if probeExists {
			return nil, cryptox.ErrEncryptionKeyMismatch
		}
		return nil, nil
	}
	ring, err := cryptox.NewKeyRing(cfg)
	if err != nil {
		return nil, cryptox.ErrEncryptionKeyMismatch
	}
	if !probeExists {
		if err := writeEncryptionProbe(ctx, data, ring, store.SystemSetting{}); err != nil {
			return nil, err
		}
		return &ring, nil
	}

	var probe encryptionProbe
	if err := json.Unmarshal(setting.ValueJSON, &probe); err != nil || strings.TrimSpace(probe.Envelope) == "" {
		return nil, cryptox.ErrEncryptionKeyMismatch
	}
	decrypted, err := ring.Decrypt(ctx, probe.Envelope, []byte(encryptionProbeAAD))
	if err != nil || subtle.ConstantTimeCompare(decrypted.Plaintext, []byte(encryptionProbePlaintext)) != 1 {
		return nil, cryptox.ErrEncryptionKeyMismatch
	}
	if decrypted.UsedPreviousKey {
		if err := writeEncryptionProbe(ctx, data, ring, setting); err != nil {
			return nil, err
		}
	}
	return &ring, nil
}

func writeEncryptionProbe(
	ctx context.Context,
	data store.Store,
	ring cryptox.KeyRing,
	existing store.SystemSetting,
) error {
	envelope, err := ring.Encrypt(ctx, []byte(encryptionProbePlaintext), []byte(encryptionProbeAAD))
	if err != nil {
		return cryptox.ErrEncryptionKeyMismatch
	}
	valueJSON, err := json.Marshal(encryptionProbe{Envelope: envelope})
	if err != nil {
		return cryptox.ErrEncryptionKeyMismatch
	}
	if existing.ID == "" {
		existing.ID = ulid.Make().String()
	}
	existing.Key = encryptionProbeSettingKey
	existing.ValueJSON = valueJSON
	existing.UpdatedAt = time.Now().UTC()
	existing.UpdatedBy = "system"
	_, err = data.Settings().Upsert(ctx, existing)
	return err
}

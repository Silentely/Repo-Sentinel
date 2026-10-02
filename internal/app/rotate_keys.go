package app

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/ai"
	"github.com/Silentely/Repo-Sentinel/internal/auth"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/cryptox"
	"github.com/Silentely/Repo-Sentinel/internal/githubx"
	"github.com/Silentely/Repo-Sentinel/internal/notify"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// RotateKeysOptions 密钥轮换选项。
type RotateKeysOptions struct {
	DryRun bool
}

// RotateKeysStats 密钥轮换统计结果。
type RotateKeysStats struct {
	ChannelsScanned int
	ChannelsRotated int
	SettingsScanned int
	SettingsRotated int
	AlreadyCurrent  int
	DryRun          bool
}

// RotateKeys 扫描并重加密所有使用上一把主密钥存储的机密凭据。
func RotateKeys(ctx context.Context, cfg config.Config, opts RotateKeysOptions) (stats RotateKeysStats, returnedErr error) {
	stats.DryRun = opts.DryRun

	if err := cfg.Validate(); err != nil {
		return stats, err
	}

	if strings.TrimSpace(cfg.Encryption.CurrentKey.Reveal()) == "" {
		return stats, newPublicError(
			"invalid_encryption_key",
			"未配置当前主密钥，无法执行轮换。",
			cryptox.ErrInvalidEncryptionKey,
		)
	}

	prevKey := strings.TrimSpace(cfg.Encryption.PreviousKey.Reveal())
	if prevKey == "" {
		// 未配置上一把密钥，不存在需要由旧换新的机密
		return stats, nil
	}

	ring, err := cryptox.NewKeyRing(cfg.Encryption)
	if err != nil {
		if errors.Is(err, cryptox.ErrInvalidEncryptionKey) {
			return stats, newPublicError(
				"invalid_encryption_key",
				"主密钥格式非法：需 64 位 hex 或 32 字节 base64。",
				cryptox.ErrInvalidEncryptionKey,
			)
		}
		return stats, newPublicError(
			"encryption_key_mismatch",
			"主密钥初始化失败。",
			cryptox.ErrEncryptionKeyMismatch,
		)
	}

	data, err := store.Open(ctx, cfg.Database)
	if err != nil {
		return stats, mapStoreOpenError(err)
	}
	defer func() {
		if closeErr := data.Close(); returnedErr == nil && closeErr != nil {
			returnedErr = newPublicError("database_unavailable", "关闭数据库资源失败。", closeErr)
		}
	}()

	// 1. 先进行只读探针校验：确认当前密钥或上一把密钥能够解密数据库探针
	if err := validateKeyRingReadOnly(ctx, data, ring); err != nil {
		return stats, err
	}

	// 2. 如果是 SQLite，获取跨进程排他锁
	if cfg.Database.Driver == "sqlite" {
		unlock, err := acquireSQLiteRotateLock(cfg.Database.URL)
		if err != nil {
			return stats, err
		}
		defer unlock()
	}

	// 3. 执行轮换（DryRun 模式下只读遍历统计，非 DryRun 在事务中执行重写）
	if opts.DryRun {
		return inspectRotateKeysReadOnly(ctx, data, ring)
	}

	err = data.WithTx(ctx, func(tx store.Store) error {
		txStats, rotateErr := executeRotateKeysInTx(ctx, tx, ring)
		if rotateErr != nil {
			return rotateErr
		}
		stats = txStats
		return nil
	})
	if err != nil {
		return stats, err
	}

	return stats, nil
}

// validateKeyRingReadOnly 只读探针校验，不产生任何数据库写操作。
func validateKeyRingReadOnly(ctx context.Context, data store.Store, ring cryptox.KeyRing) error {
	setting, err := data.Settings().Get(ctx, encryptionProbeSettingKey)
	if err != nil {
		if errors.Is(err, store.ErrNotFound) {
			return nil
		}
		return newPublicError("database_unavailable", "无法读取加密探针设置。", err)
	}
	var probe encryptionProbe
	if err := json.Unmarshal(setting.ValueJSON, &probe); err != nil || strings.TrimSpace(probe.Envelope) == "" {
		return newPublicError("encryption_key_mismatch", "数据库加密探针数据损坏或为空。", cryptox.ErrEncryptionKeyMismatch)
	}
	decrypted, err := ring.Decrypt(ctx, probe.Envelope, []byte(encryptionProbeAAD))
	if err != nil || string(decrypted.Plaintext) != encryptionProbePlaintext {
		return newPublicError("encryption_key_mismatch", "当前与上一把主密钥均无法解密数据库探针。", cryptox.ErrEncryptionKeyMismatch)
	}
	return nil
}

// inspectRotateKeysReadOnly 只读检查需要轮换的记录数量。
func inspectRotateKeysReadOnly(ctx context.Context, data store.Store, ring cryptox.KeyRing) (RotateKeysStats, error) {
	stats := RotateKeysStats{DryRun: true}

	// 检查通知渠道
	channels, err := data.Channels().List(ctx)
	if err != nil {
		return stats, err
	}
	for _, ch := range channels {
		if strings.TrimSpace(ch.SecretEnvelope) == "" {
			continue
		}
		stats.ChannelsScanned++
		dec, err := ring.Decrypt(ctx, ch.SecretEnvelope, []byte(notify.AAD))
		if err != nil {
			return stats, fmt.Errorf("decrypt channel %s: %w", ch.ID, err)
		}
		if dec.UsedPreviousKey {
			stats.ChannelsRotated++
		} else {
			stats.AlreadyCurrent++
		}
	}

	// 检查 GitHub Runtime
	if setting, err := data.Settings().Get(ctx, githubx.RuntimeSettingKey); err == nil {
		stats.SettingsScanned++
		var ghStored githubx.StoredRuntime
		if err := json.Unmarshal(setting.ValueJSON, &ghStored); err == nil {
			needsRotate := false
			if ghStored.PrivateKeyPEMEnvelope != "" {
				dec, err := ring.Decrypt(ctx, ghStored.PrivateKeyPEMEnvelope, []byte("reposentinel:github-runtime:v1"))
				if err != nil {
					return stats, fmt.Errorf("decrypt github private key: %w", err)
				}
				if dec.UsedPreviousKey {
					needsRotate = true
				}
			}
			if ghStored.WebhookSecretEnvelope != "" {
				dec, err := ring.Decrypt(ctx, ghStored.WebhookSecretEnvelope, []byte("reposentinel:github-runtime:v1"))
				if err != nil {
					return stats, fmt.Errorf("decrypt github webhook secret: %w", err)
				}
				if dec.UsedPreviousKey {
					needsRotate = true
				}
			}
			if needsRotate {
				stats.SettingsRotated++
			} else if ghStored.PrivateKeyPEMEnvelope != "" || ghStored.WebhookSecretEnvelope != "" {
				stats.AlreadyCurrent++
			}
		}
	}

	// 检查 AI Runtime
	if setting, err := data.Settings().Get(ctx, ai.RuntimeSettingKey); err == nil {
		stats.SettingsScanned++
		var aiStored ai.StoredConfig
		if err := json.Unmarshal(setting.ValueJSON, &aiStored); err == nil && aiStored.APIKeyEnvelope != "" {
			dec, err := ring.Decrypt(ctx, aiStored.APIKeyEnvelope, []byte("reposentinel:ai-runtime:v1"))
			if err != nil {
				return stats, fmt.Errorf("decrypt ai api key: %w", err)
			}
			if dec.UsedPreviousKey {
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	// 检查 TOTP
	if setting, err := data.Settings().Get(ctx, auth.TOTPSettingKey); err == nil {
		stats.SettingsScanned++
		var totpStored auth.StoredTOTPConfig
		if err := json.Unmarshal(setting.ValueJSON, &totpStored); err == nil && totpStored.SecretEnvelope != "" {
			dec, err := ring.Decrypt(ctx, totpStored.SecretEnvelope, []byte("reposentinel:totp-secret:v1"))
			if err != nil {
				return stats, fmt.Errorf("decrypt totp secret: %w", err)
			}
			if dec.UsedPreviousKey {
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	// 检查探针
	if setting, err := data.Settings().Get(ctx, encryptionProbeSettingKey); err == nil {
		stats.SettingsScanned++
		var probe encryptionProbe
		if err := json.Unmarshal(setting.ValueJSON, &probe); err == nil && probe.Envelope != "" {
			dec, err := ring.Decrypt(ctx, probe.Envelope, []byte(encryptionProbeAAD))
			if err != nil {
				return stats, fmt.Errorf("decrypt encryption probe: %w", err)
			}
			if dec.UsedPreviousKey {
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	return stats, nil
}

// executeRotateKeysInTx 在单事务内完成解密、重加密与写回。
func executeRotateKeysInTx(ctx context.Context, tx store.Store, ring cryptox.KeyRing) (RotateKeysStats, error) {
	stats := RotateKeysStats{DryRun: false}

	// 1. 轮换通知渠道
	channels, err := tx.Channels().List(ctx)
	if err != nil {
		return stats, err
	}
	for _, ch := range channels {
		if strings.TrimSpace(ch.SecretEnvelope) == "" {
			continue
		}
		stats.ChannelsScanned++
		dec, err := ring.Decrypt(ctx, ch.SecretEnvelope, []byte(notify.AAD))
		if err != nil {
			return stats, fmt.Errorf("decrypt channel %s: %w", ch.ID, err)
		}
		if dec.UsedPreviousKey {
			newEnvelope, err := ring.Encrypt(ctx, dec.Plaintext, []byte(notify.AAD))
			if err != nil {
				return stats, fmt.Errorf("re-encrypt channel %s: %w", ch.ID, err)
			}
			ch.SecretEnvelope = newEnvelope
			if _, err := tx.Channels().Upsert(ctx, ch); err != nil {
				return stats, fmt.Errorf("save rotated channel %s: %w", ch.ID, err)
			}
			stats.ChannelsRotated++
		} else {
			stats.AlreadyCurrent++
		}
	}

	// 2. 轮换 GitHub Runtime
	if setting, err := tx.Settings().Get(ctx, githubx.RuntimeSettingKey); err == nil {
		stats.SettingsScanned++
		var ghStored githubx.StoredRuntime
		if err := json.Unmarshal(setting.ValueJSON, &ghStored); err == nil {
			modified := false
			if ghStored.PrivateKeyPEMEnvelope != "" {
				dec, err := ring.Decrypt(ctx, ghStored.PrivateKeyPEMEnvelope, []byte("reposentinel:github-runtime:v1"))
				if err != nil {
					return stats, fmt.Errorf("decrypt github private key: %w", err)
				}
				if dec.UsedPreviousKey {
					newEnv, err := ring.Encrypt(ctx, dec.Plaintext, []byte("reposentinel:github-runtime:v1"))
					if err != nil {
						return stats, fmt.Errorf("re-encrypt github private key: %w", err)
					}
					ghStored.PrivateKeyPEMEnvelope = newEnv
					modified = true
				}
			}
			if ghStored.WebhookSecretEnvelope != "" {
				dec, err := ring.Decrypt(ctx, ghStored.WebhookSecretEnvelope, []byte("reposentinel:github-runtime:v1"))
				if err != nil {
					return stats, fmt.Errorf("decrypt github webhook secret: %w", err)
				}
				if dec.UsedPreviousKey {
					newEnv, err := ring.Encrypt(ctx, dec.Plaintext, []byte("reposentinel:github-runtime:v1"))
					if err != nil {
						return stats, fmt.Errorf("re-encrypt github webhook secret: %w", err)
					}
					ghStored.WebhookSecretEnvelope = newEnv
					modified = true
				}
			}
			if modified {
				raw, err := json.Marshal(ghStored)
				if err != nil {
					return stats, err
				}
				setting.ValueJSON = raw
				setting.UpdatedAt = time.Now().UTC()
				if _, err := tx.Settings().Upsert(ctx, setting); err != nil {
					return stats, fmt.Errorf("save rotated github runtime: %w", err)
				}
				stats.SettingsRotated++
			} else if ghStored.PrivateKeyPEMEnvelope != "" || ghStored.WebhookSecretEnvelope != "" {
				stats.AlreadyCurrent++
			}
		}
	}

	// 3. 轮换 AI Runtime
	if setting, err := tx.Settings().Get(ctx, ai.RuntimeSettingKey); err == nil {
		stats.SettingsScanned++
		var aiStored ai.StoredConfig
		if err := json.Unmarshal(setting.ValueJSON, &aiStored); err == nil && aiStored.APIKeyEnvelope != "" {
			dec, err := ring.Decrypt(ctx, aiStored.APIKeyEnvelope, []byte("reposentinel:ai-runtime:v1"))
			if err != nil {
				return stats, fmt.Errorf("decrypt ai api key: %w", err)
			}
			if dec.UsedPreviousKey {
				newEnv, err := ring.Encrypt(ctx, dec.Plaintext, []byte("reposentinel:ai-runtime:v1"))
				if err != nil {
					return stats, fmt.Errorf("re-encrypt ai api key: %w", err)
				}
				aiStored.APIKeyEnvelope = newEnv
				raw, err := json.Marshal(aiStored)
				if err != nil {
					return stats, err
				}
				setting.ValueJSON = raw
				setting.UpdatedAt = time.Now().UTC()
				if _, err := tx.Settings().Upsert(ctx, setting); err != nil {
					return stats, fmt.Errorf("save rotated ai config: %w", err)
				}
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	// 4. 轮换 TOTP
	if setting, err := tx.Settings().Get(ctx, auth.TOTPSettingKey); err == nil {
		stats.SettingsScanned++
		var totpStored auth.StoredTOTPConfig
		if err := json.Unmarshal(setting.ValueJSON, &totpStored); err == nil && totpStored.SecretEnvelope != "" {
			dec, err := ring.Decrypt(ctx, totpStored.SecretEnvelope, []byte("reposentinel:totp-secret:v1"))
			if err != nil {
				return stats, fmt.Errorf("decrypt totp secret: %w", err)
			}
			if dec.UsedPreviousKey {
				newEnv, err := ring.Encrypt(ctx, dec.Plaintext, []byte("reposentinel:totp-secret:v1"))
				if err != nil {
					return stats, fmt.Errorf("re-encrypt totp secret: %w", err)
				}
				totpStored.SecretEnvelope = newEnv
				raw, err := json.Marshal(totpStored)
				if err != nil {
					return stats, err
				}
				setting.ValueJSON = raw
				setting.UpdatedAt = time.Now().UTC()
				if _, err := tx.Settings().Upsert(ctx, setting); err != nil {
					return stats, fmt.Errorf("save rotated totp config: %w", err)
				}
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	// 5. 轮换探针
	if setting, err := tx.Settings().Get(ctx, encryptionProbeSettingKey); err == nil {
		stats.SettingsScanned++
		var probe encryptionProbe
		if err := json.Unmarshal(setting.ValueJSON, &probe); err == nil && probe.Envelope != "" {
			dec, err := ring.Decrypt(ctx, probe.Envelope, []byte(encryptionProbeAAD))
			if err != nil {
				return stats, fmt.Errorf("decrypt encryption probe: %w", err)
			}
			if dec.UsedPreviousKey {
				if err := writeEncryptionProbe(ctx, tx, ring, setting); err != nil {
					return stats, fmt.Errorf("save rotated probe: %w", err)
				}
				stats.SettingsRotated++
			} else {
				stats.AlreadyCurrent++
			}
		}
	}

	return stats, nil
}

// acquireSQLiteRotateLock 获取 SQLite 密钥轮换文件排他锁。
func acquireSQLiteRotateLock(rawURL string) (unlock func(), err error) {
	// 剔除 file: 前缀与查询参数
	clean := strings.TrimPrefix(rawURL, "file:")
	if idx := strings.Index(clean, "?"); idx >= 0 {
		clean = clean[:idx]
	}
	clean = strings.TrimSpace(clean)
	if clean == "" || clean == ":memory:" || strings.Contains(rawURL, "mode=memory") {
		return func() {}, nil
	}

	lockPath := clean + ".rotate.lock"
	f, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0600)
	if err != nil {
		return nil, newPublicError("lock_failed", fmt.Sprintf("无法创建密钥轮换锁文件: %v", err), err)
	}

	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		if errors.Is(err, syscall.EWOULDBLOCK) || errors.Is(err, syscall.EAGAIN) {
			return nil, newPublicError("lock_held", "另一进程正在执行密钥轮换或持有锁，请稍候重试。", err)
		}
		return nil, newPublicError("lock_failed", fmt.Sprintf("获取排他锁失败: %v", err), err)
	}

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
		_ = os.Remove(lockPath)
	}, nil
}

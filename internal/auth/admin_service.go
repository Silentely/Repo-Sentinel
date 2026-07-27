package auth

import (
	"context"
	"encoding/json"
	"errors"
	"strings"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	auditAdminBootstrapped     = "admin_bootstrapped"
	auditAdminPasswordChanged  = "admin_password_changed"
	auditAdminPasswordResetCLI = "admin_password_reset_cli"

	dummyPasswordPHC = "$argon2id$v=19$m=65536,t=3,p=2$MDEyMzQ1Njc4OWFiY2RlZg$" +
		"AAAAAAAAAA" + "AAAAAAAAAA" + "AAAAAAAAAA" + "AAAAAAAAAA" + "AAA"
)

// Admin 是认证调用方可见的最小管理员模型。
type Admin struct {
	ID       string
	Username string
}

// AdminService 编排唯一管理员、密码、Session 撤销与安全审计。
type AdminService struct {
	data      store.Store
	passwords PasswordHasher
}

// NewAdminService 创建唯一管理员认证服务。
func NewAdminService(data store.Store, passwords PasswordHasher) *AdminService {
	return &AdminService{data: data, passwords: passwords}
}

// BootstrapAdmin 原子创建唯一管理员并记录系统引导审计。
func (s *AdminService) BootstrapAdmin(
	ctx context.Context,
	username, password string,
) (Admin, error) {
	username = strings.TrimSpace(username)
	if username == "" {
		return Admin{}, ErrValidationFailed
	}
	passwordHash, err := s.passwords.Hash(password)
	if err != nil {
		return Admin{}, err
	}

	now := time.Now().UTC()
	input := store.AdminAccount{
		ID:                ulid.Make().String(),
		Username:          username,
		PasswordHash:      passwordHash,
		CreatedAt:         now,
		UpdatedAt:         now,
		PasswordChangedAt: now,
	}
	var created store.AdminAccount
	err = s.data.WithTx(ctx, func(txStore store.Store) error {
		created, err = txStore.Admins().Create(ctx, input)
		if err != nil {
			return mapBootstrapError(err)
		}
		_, err = txStore.Audits().Append(ctx, adminAudit(
			auditAdminBootstrapped,
			"system",
			"system",
			created.ID,
			now,
		))
		return err
	})
	if err != nil {
		return Admin{}, mapBootstrapError(err)
	}
	return publicAdmin(created), nil
}

// Authenticate 对未知用户名、错误密码与损坏存储 PHC 返回同一凭据错误。
func (s *AdminService) Authenticate(
	ctx context.Context,
	username, password string,
) (Admin, error) {
	account, err := s.data.Admins().FindByUsername(ctx, strings.TrimSpace(username))
	if errors.Is(err, store.ErrNotFound) {
		_, _ = s.passwords.Verify(dummyPasswordPHC, password)
		return Admin{}, ErrInvalidCredentials
	}
	if err != nil {
		return Admin{}, err
	}

	verified, err := s.passwords.Verify(account.PasswordHash, password)
	if err != nil || !verified {
		return Admin{}, ErrInvalidCredentials
	}
	return publicAdmin(account), nil
}

// ChangePassword 验证当前密码，更新哈希并仅保留当前 Session。
func (s *AdminService) ChangePassword(
	ctx context.Context,
	adminID, currentSessionID, currentPassword, newPassword string,
) error {
	currentSessionID = strings.TrimSpace(currentSessionID)
	if currentSessionID == "" {
		return ErrValidationFailed
	}

	account, err := s.data.Admins().Get(ctx, adminID)
	if errors.Is(err, store.ErrNotFound) {
		return ErrInvalidCredentials
	}
	if err != nil {
		return err
	}
	verified, err := s.passwords.Verify(account.PasswordHash, currentPassword)
	if err != nil || !verified {
		return ErrInvalidCredentials
	}
	passwordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.data.WithTx(ctx, func(txStore store.Store) error {
		updated, err := txStore.Admins().UpdatePasswordIfCurrent(
			ctx,
			account.ID,
			account.PasswordHash,
			passwordHash,
			now,
		)
		if err != nil {
			return err
		}
		if !updated {
			return ErrInvalidCredentials
		}
		if _, err := txStore.Sessions().DeleteOthers(ctx, account.ID, currentSessionID); err != nil {
			return err
		}
		_, err = txStore.Audits().Append(ctx, adminAudit(
			auditAdminPasswordChanged,
			"admin",
			account.ID,
			account.ID,
			now,
		))
		return err
	})
}

// ResetPassword 为本机 CLI 更新唯一管理员密码并撤销全部 Session。
func (s *AdminService) ResetPassword(ctx context.Context, newPassword string) error {
	passwordHash, err := s.passwords.Hash(newPassword)
	if err != nil {
		return err
	}

	now := time.Now().UTC()
	return s.data.WithTx(ctx, func(txStore store.Store) error {
		account, err := txStore.Admins().GetOnly(ctx)
		if err != nil {
			return err
		}
		if _, err := txStore.Admins().UpdatePassword(ctx, account.ID, passwordHash, now); err != nil {
			return err
		}
		if _, err := txStore.Sessions().DeleteOthers(ctx, account.ID, ""); err != nil {
			return err
		}
		_, err = txStore.Audits().Append(ctx, adminAudit(
			auditAdminPasswordResetCLI,
			"cli",
			"local",
			account.ID,
			now,
		))
		return err
	})
}

func publicAdmin(account store.AdminAccount) Admin {
	return Admin{ID: account.ID, Username: account.Username}
}

func mapBootstrapError(err error) error {
	if errors.Is(err, store.ErrConflict) {
		return ErrConflict
	}
	return err
}

func adminAudit(action, actorType, actorID, targetID string, createdAt time.Time) store.AuditLog {
	return store.AuditLog{
		ID:           ulid.Make().String(),
		Action:       action,
		ActorType:    actorType,
		ActorID:      actorID,
		TargetType:   "admin",
		TargetID:     targetID,
		MetadataJSON: json.RawMessage(`{}`),
		IPAddress:    "",
		CreatedAt:    createdAt.UTC(),
	}
}

package auth

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

const (
	adminInitialPassword = "初始管理员密码一二三四五六"
	adminChangedPassword = "更新管理员密码一二三四五六"
	adminResetPassword   = "命令行重置密码一二三四五六"
	adminWrongPassword   = "错误管理员密码一二三四五六"
)

// 此测试能捕获公开管理员模型意外暴露 PasswordHash 或其他持久化敏感字段。
func Test管理员公开模型只包含调用方需要的字段(t *testing.T) {
	typeOfAdmin := reflect.TypeOf(Admin{})
	if typeOfAdmin.NumField() != 2 {
		t.Fatalf("Admin 字段数=%d，期望仅包含 ID 与 Username", typeOfAdmin.NumField())
	}
	for _, field := range []string{"ID", "Username"} {
		if _, exists := typeOfAdmin.FieldByName(field); !exists {
			t.Fatalf("Admin 缺少字段 %s", field)
		}
	}
	if _, exists := typeOfAdmin.FieldByName("PasswordHash"); exists {
		t.Fatal("Admin 不得暴露 PasswordHash")
	}
}

// 此测试能捕获空用户名未校验、引导未原子写审计、用户名未规范化认证或第二个管理员未映射 conflict。
func Test管理员引导创建唯一账号并支持规范化认证(t *testing.T) {
	opened := openAuthStore(t)
	service := NewAdminService(opened, NewPasswordHasher())

	if _, err := service.BootstrapAdmin(t.Context(), "  ", adminInitialPassword); !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("空用户名引导错误=%v，期望 validation_failed", err)
	}
	if _, err := opened.Admins().GetOnly(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("空用户名引导后 GetOnly 错误=%v，期望 ErrNotFound", err)
	}

	admin, err := service.BootstrapAdmin(t.Context(), "  Repo Admin  ", adminInitialPassword)
	if err != nil {
		t.Fatalf("引导管理员失败: %v", err)
	}
	if admin.ID == "" || admin.Username != "Repo Admin" {
		t.Fatalf("管理员公开结果 ID 是否为空=%v，用户名=%q", admin.ID == "", admin.Username)
	}
	if _, err := ulid.ParseStrict(admin.ID); err != nil {
		t.Fatal("管理员 ID 不是合法 ULID")
	}

	stored, err := opened.Admins().GetOnly(t.Context())
	if err != nil {
		t.Fatalf("读取唯一管理员失败: %v", err)
	}
	if stored.ID != admin.ID || stored.Username != admin.Username || stored.PasswordHash == "" || stored.PasswordHash == adminInitialPassword {
		t.Fatal("持久化管理员与公开结果不一致，或密码未安全哈希")
	}
	verified, err := NewPasswordHasher().Verify(stored.PasswordHash, adminInitialPassword)
	if err != nil || !verified {
		t.Fatalf("持久化密码验证结果=(%v, %v)，期望通过", verified, err)
	}

	authenticated, err := service.Authenticate(t.Context(), "  repo ADMIN  ", adminInitialPassword)
	if err != nil || authenticated != admin {
		t.Fatalf("规范化用户名认证结果=(ID匹配:%v, 用户名:%q, 错误:%v)", authenticated.ID == admin.ID, authenticated.Username, err)
	}

	if _, err := service.BootstrapAdmin(t.Context(), "Another Admin", adminChangedPassword); !errors.Is(err, ErrConflict) {
		t.Fatalf("第二次引导错误=%v，期望 conflict", err)
	}

	audits := listAuthAudits(t, opened)
	if len(audits) != 1 {
		t.Fatalf("引导审计数量=%d，期望 1", len(audits))
	}
	assertAudit(t, audits[0], "admin_bootstrapped", "system", "system", admin.ID)
}

// 此测试能捕获未知用户名、错误密码或损坏存储 PHC 产生可枚举的不同认证错误。
func Test管理员认证统一隐藏未知账号错误密码与损坏PHC(t *testing.T) {
	opened := openAuthStore(t)
	service := NewAdminService(opened, NewPasswordHasher())
	if _, err := service.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword); err != nil {
		t.Fatalf("准备管理员失败: %v", err)
	}

	_, wrongPasswordErr := service.Authenticate(t.Context(), "Repo Admin", adminWrongPassword)
	_, unknownUsernameErr := service.Authenticate(t.Context(), "Unknown Admin", adminWrongPassword)
	if !errors.Is(wrongPasswordErr, ErrInvalidCredentials) || !errors.Is(unknownUsernameErr, ErrInvalidCredentials) {
		t.Fatalf("认证错误=(错误密码:%v, 未知账号:%v)，期望统一 invalid_credentials", wrongPasswordErr, unknownUsernameErr)
	}
	if wrongPasswordErr.Error() != unknownUsernameErr.Error() {
		t.Fatal("未知账号与错误密码返回了可区分的错误文本")
	}

	damagedStore := openAuthStore(t)
	now := time.Now().UTC()
	if _, err := damagedStore.Admins().Create(t.Context(), store.AdminAccount{
		ID:                ulid.Make().String(),
		Username:          "Damaged Admin",
		PasswordHash:      "damaged-password-hash",
		CreatedAt:         now,
		UpdatedAt:         now,
		PasswordChangedAt: now,
	}); err != nil {
		t.Fatalf("准备损坏哈希管理员失败: %v", err)
	}
	damagedService := NewAdminService(damagedStore, NewPasswordHasher())
	if _, err := damagedService.Authenticate(t.Context(), "Damaged Admin", adminInitialPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("损坏存储 PHC 的认证错误=%v，期望 invalid_credentials", err)
	}
}

// 此测试能捕获固定 dummy PHC 拼写损坏，导致未知用户未执行同类 Argon2id Verify。
func Test管理员未知用户使用的固定DummyPHC合法(t *testing.T) {
	verified, err := NewPasswordHasher().Verify(dummyPasswordPHC, adminWrongPassword)
	if err != nil || verified {
		t.Fatalf("固定 dummy PHC 验证结果=(%v, %v)，期望正常比较后不匹配", verified, err)
	}
}

// 此测试能捕获空当前 Session 被接受、旧密码未验证、密码未更新、误删当前 Session 或遗漏安全审计。
func Test管理员修改密码仅保留当前Session(t *testing.T) {
	opened := openAuthStore(t)
	service := NewAdminService(opened, NewPasswordHasher())
	admin, err := service.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword)
	if err != nil {
		t.Fatalf("准备管理员失败: %v", err)
	}
	now := time.Now().UTC()
	current := createAuthSession(t, opened, admin.ID, "01K00000000000000000000101", "current-session-token", now)
	other := createAuthSession(t, opened, admin.ID, "01K00000000000000000000102", "other-session-token", now)
	before, err := opened.Admins().GetOnly(t.Context())
	if err != nil {
		t.Fatalf("读取改密前管理员失败: %v", err)
	}

	if err := service.ChangePassword(t.Context(), admin.ID, "   ", adminInitialPassword, adminChangedPassword); !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("空当前 Session 改密错误=%v，期望 validation_failed", err)
	}
	if err := service.ChangePassword(t.Context(), admin.ID, current.ID, adminWrongPassword, adminChangedPassword); !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("错误旧密码改密错误=%v，期望 invalid_credentials", err)
	}
	unchanged, err := opened.Admins().GetOnly(t.Context())
	if err != nil || unchanged.PasswordHash != before.PasswordHash {
		t.Fatal("失败的改密不应改变密码哈希")
	}
	assertSessionActive(t, opened, current, now)
	assertSessionActive(t, opened, other, now)

	if err := service.ChangePassword(t.Context(), admin.ID, current.ID, adminInitialPassword, adminChangedPassword); err != nil {
		t.Fatalf("修改密码失败: %v", err)
	}
	updated, err := opened.Admins().GetOnly(t.Context())
	if err != nil {
		t.Fatalf("读取改密后管理员失败: %v", err)
	}
	if updated.PasswordHash == before.PasswordHash || updated.PasswordChangedAt.Location() != time.UTC {
		t.Fatal("成功改密应替换哈希并使用 UTC 变更时间")
	}
	verified, err := NewPasswordHasher().Verify(updated.PasswordHash, adminChangedPassword)
	if err != nil || !verified {
		t.Fatalf("新密码验证结果=(%v, %v)，期望通过", verified, err)
	}
	verified, err = NewPasswordHasher().Verify(updated.PasswordHash, adminInitialPassword)
	if err != nil || verified {
		t.Fatalf("旧密码验证结果=(%v, %v)，期望不通过且无错误", verified, err)
	}
	assertSessionActive(t, opened, current, now)
	assertSessionMissing(t, opened, other, now)
	assertAudit(t, requireAudit(t, opened, "admin_password_changed"), "admin_password_changed", "admin", admin.ID, admin.ID)
}

// 此测试能捕获 CLI 重置依赖 Web 凭据、未通过 GetOnly 找管理员、未撤销全部 Session 或审计身份错误。
func Test管理员CLI重置密码撤销全部Session(t *testing.T) {
	opened := openAuthStore(t)
	service := NewAdminService(opened, NewPasswordHasher())
	admin, err := service.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword)
	if err != nil {
		t.Fatalf("准备管理员失败: %v", err)
	}
	now := time.Now().UTC()
	first := createAuthSession(t, opened, admin.ID, "01K00000000000000000000111", "reset-session-token-1", now)
	second := createAuthSession(t, opened, admin.ID, "01K00000000000000000000112", "reset-session-token-2", now)

	if err := service.ResetPassword(t.Context(), "太短"); !errors.Is(err, ErrValidationFailed) {
		t.Fatalf("短密码重置错误=%v，期望 validation_failed", err)
	}
	assertSessionActive(t, opened, first, now)
	assertSessionActive(t, opened, second, now)

	if err := service.ResetPassword(t.Context(), adminResetPassword); err != nil {
		t.Fatalf("CLI 重置密码失败: %v", err)
	}
	updated, err := opened.Admins().GetOnly(t.Context())
	if err != nil {
		t.Fatalf("读取重置后管理员失败: %v", err)
	}
	verified, err := NewPasswordHasher().Verify(updated.PasswordHash, adminResetPassword)
	if err != nil || !verified {
		t.Fatalf("重置密码验证结果=(%v, %v)，期望通过", verified, err)
	}
	assertSessionMissing(t, opened, first, now)
	assertSessionMissing(t, opened, second, now)
	assertAudit(t, requireAudit(t, opened, "admin_password_reset_cli"), "admin_password_reset_cli", "cli", "local", admin.ID)
}

// 此测试能捕获已验证旧密码的改密请求覆盖并发 CLI 重置结果，并错误写入改密审计。
func Test管理员并发重置使已验证旧密码的修改密码失效(t *testing.T) {
	opened := openAuthStore(t)
	resetService := NewAdminService(opened, NewPasswordHasher())
	admin, err := resetService.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword)
	if err != nil {
		t.Fatalf("准备管理员失败: %v", err)
	}
	now := time.Now().UTC()
	current := createAuthSession(t, opened, admin.ID, "01K00000000000000000000131", "concurrent-current-token", now)
	other := createAuthSession(t, opened, admin.ID, "01K00000000000000000000132", "concurrent-other-token", now)

	entered := make(chan struct{}, 1)
	release := make(chan struct{})
	releaseChange := func() {
		select {
		case <-release:
		default:
			close(release)
		}
	}
	t.Cleanup(releaseChange)
	changeService := NewAdminService(gatedWithTxStore{
		Store:   opened,
		entered: entered,
		release: release,
	}, NewPasswordHasher())
	changeResult := make(chan error, 1)
	go func() {
		changeResult <- changeService.ChangePassword(
			t.Context(),
			admin.ID,
			current.ID,
			adminInitialPassword,
			adminChangedPassword,
		)
	}()

	select {
	case <-entered:
		// WithTx 尚未委托给真实 Store，因此旧密码已验证且改密写事务尚未开始。
	case <-time.After(10 * time.Second):
		t.Fatal("等待并发改密进入事务门控超时")
	}
	if err := resetService.ResetPassword(t.Context(), adminResetPassword); err != nil {
		t.Fatalf("并发 CLI 重置密码失败: %v", err)
	}
	releaseChange()

	select {
	case err = <-changeResult:
	case <-time.After(10 * time.Second):
		t.Fatal("等待并发改密返回超时")
	}
	if !errors.Is(err, ErrInvalidCredentials) {
		t.Fatalf("并发改密错误=%v，期望 invalid_credentials", err)
	}

	updated, err := opened.Admins().GetOnly(t.Context())
	if err != nil {
		t.Fatalf("读取并发操作后管理员失败: %v", err)
	}
	verified, err := NewPasswordHasher().Verify(updated.PasswordHash, adminResetPassword)
	if err != nil || !verified {
		t.Fatalf("CLI 重置后的密码验证结果=(%v, %v)，期望通过", verified, err)
	}
	verified, err = NewPasswordHasher().Verify(updated.PasswordHash, adminChangedPassword)
	if err != nil || verified {
		t.Fatalf("失效改密的新密码验证结果=(%v, %v)，期望不通过且无错误", verified, err)
	}
	assertSessionMissing(t, opened, current, now)
	assertSessionMissing(t, opened, other, now)
	audits := listAuthAudits(t, opened)
	resetAudit, found := findAudit(audits, "admin_password_reset_cli")
	if !found {
		t.Fatal("并发操作后缺少 CLI 重置审计")
	}
	assertAudit(t, resetAudit, "admin_password_reset_cli", "cli", "local", admin.ID)
	if _, found := findAudit(audits, "admin_password_changed"); found {
		t.Fatal("失效的并发改密不应写入改密审计")
	}
}

// 此测试能捕获管理员创建发生在事务外，导致审计追加失败后仍残留账号。
func Test管理员引导在审计失败时回滚账号(t *testing.T) {
	opened := openAuthStore(t)
	injectedErr := errors.New("audit append unavailable")
	service := NewAdminService(auditFailureStore{Store: opened, err: injectedErr}, NewPasswordHasher())

	if _, err := service.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword); !errors.Is(err, injectedErr) {
		t.Fatalf("引导错误=%v，期望保留审计失败", err)
	}
	if _, err := opened.Admins().GetOnly(t.Context()); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("审计失败后 GetOnly 错误=%v，期望账号已回滚", err)
	}
	if audits := listAuthAudits(t, opened); len(audits) != 0 {
		t.Fatalf("审计失败后持久化审计数量=%d，期望 0", len(audits))
	}
}

// 此测试能捕获改密或 CLI 重置的密码更新、Session 撤销与审计未处于同一事务。
func Test管理员密码写操作在审计失败时回滚全部副作用(t *testing.T) {
	for _, tc := range []struct {
		name      string
		operation func(context.Context, *AdminService, Admin, store.AdminSession) error
	}{
		{
			name: "修改密码",
			operation: func(ctx context.Context, service *AdminService, admin Admin, current store.AdminSession) error {
				return service.ChangePassword(ctx, admin.ID, current.ID, adminInitialPassword, adminChangedPassword)
			},
		},
		{
			name: "CLI重置密码",
			operation: func(ctx context.Context, service *AdminService, _ Admin, _ store.AdminSession) error {
				return service.ResetPassword(ctx, adminResetPassword)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			opened := openAuthStore(t)
			normalService := NewAdminService(opened, NewPasswordHasher())
			admin, err := normalService.BootstrapAdmin(t.Context(), "Repo Admin", adminInitialPassword)
			if err != nil {
				t.Fatalf("准备管理员失败: %v", err)
			}
			now := time.Now().UTC()
			current := createAuthSession(t, opened, admin.ID, "01K00000000000000000000121", "rollback-current-token", now)
			other := createAuthSession(t, opened, admin.ID, "01K00000000000000000000122", "rollback-other-token", now)
			before, err := opened.Admins().GetOnly(t.Context())
			if err != nil {
				t.Fatalf("读取操作前管理员失败: %v", err)
			}

			injectedErr := errors.New("audit append unavailable")
			failingService := NewAdminService(auditFailureStore{Store: opened, err: injectedErr}, NewPasswordHasher())
			if err := tc.operation(t.Context(), failingService, admin, current); !errors.Is(err, injectedErr) {
				t.Fatalf("写操作错误=%v，期望保留审计失败", err)
			}

			after, err := opened.Admins().GetOnly(t.Context())
			if err != nil || after.PasswordHash != before.PasswordHash || !after.PasswordChangedAt.Equal(before.PasswordChangedAt) {
				t.Fatal("审计失败后密码更新未完整回滚")
			}
			assertSessionActive(t, opened, current, now)
			assertSessionActive(t, opened, other, now)
			if _, found := findAudit(listAuthAudits(t, opened), "admin_password_changed"); found {
				t.Fatal("审计失败后不应存在改密审计")
			}
			if _, found := findAudit(listAuthAudits(t, opened), "admin_password_reset_cli"); found {
				t.Fatal("审计失败后不应存在 CLI 重置审计")
			}
		})
	}
}

func openAuthStore(t *testing.T) store.Store {
	t.Helper()
	// Atlas 直接接收 sql.DB 时使用 TMPDIR 下的固定 SQLite 锁名；每个测试必须隔离该锁。
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          "file:" + filepath.Join(temporaryDir, "auth.db"),
		MaxOpenConns: 1,
		MaxIdleConns: 1,
	})
	if err != nil {
		t.Fatalf("打开认证测试 Store 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := opened.Close(); err != nil {
			t.Errorf("关闭认证测试 Store 失败: %v", err)
		}
	})
	return opened
}

func createAuthSession(
	t *testing.T,
	opened store.Store,
	adminID, id, tokenHash string,
	now time.Time,
) store.AdminSession {
	t.Helper()
	session, err := opened.Sessions().Create(t.Context(), store.AdminSession{
		ID:         id,
		AdminID:    adminID,
		TokenHash:  tokenHash,
		CSRFHash:   "csrf-" + tokenHash,
		CreatedAt:  now.Add(-time.Minute),
		ExpiresAt:  now.Add(time.Hour),
		LastSeenAt: now.Add(-time.Minute),
		IPAddress:  "127.0.0.1",
		UserAgent:  "auth-test",
	})
	if err != nil {
		t.Fatalf("创建认证测试 Session 失败: %v", err)
	}
	return session
}

func assertSessionActive(t *testing.T, opened store.Store, session store.AdminSession, now time.Time) {
	t.Helper()
	found, err := opened.Sessions().GetActiveByTokenHash(t.Context(), session.TokenHash, now)
	if err != nil || found.ID != session.ID {
		t.Fatalf("Session %s 应保持活动，错误=%v", session.ID, err)
	}
}

func assertSessionMissing(t *testing.T, opened store.Store, session store.AdminSession, now time.Time) {
	t.Helper()
	if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
		t.Fatalf("Session %s 应已撤销，错误=%v", session.ID, err)
	}
}

func listAuthAudits(t *testing.T, opened store.Store) []store.AuditLog {
	t.Helper()
	audits, err := opened.Audits().List(t.Context(), 100, 0)
	if err != nil {
		t.Fatalf("列出认证审计失败: %v", err)
	}
	return audits
}

func requireAudit(t *testing.T, opened store.Store, action string) store.AuditLog {
	t.Helper()
	audit, found := findAudit(listAuthAudits(t, opened), action)
	if !found {
		t.Fatalf("未找到审计动作 %s", action)
	}
	return audit
}

func findAudit(audits []store.AuditLog, action string) (store.AuditLog, bool) {
	for _, audit := range audits {
		if audit.Action == action {
			return audit, true
		}
	}
	return store.AuditLog{}, false
}

func assertAudit(
	t *testing.T,
	audit store.AuditLog,
	action, actorType, actorID, targetID string,
) {
	t.Helper()
	if audit.Action != action || audit.ActorType != actorType || audit.ActorID != actorID ||
		audit.TargetType != "admin" || audit.TargetID != targetID {
		t.Fatal("认证审计的动作、参与者或目标不符合固定契约")
	}
	if !bytes.Equal(audit.MetadataJSON, []byte(`{}`)) || audit.IPAddress != "" {
		t.Fatal("认证审计 metadata 必须为空 JSON 且 IP 必须为空")
	}
	if audit.CreatedAt.Location() != time.UTC {
		t.Fatal("认证审计时间必须使用 UTC")
	}
	if _, err := ulid.ParseStrict(audit.ID); err != nil {
		t.Fatal("认证审计 ID 不是合法 ULID")
	}
}

type auditFailureStore struct {
	store.Store
	err error
}

type gatedWithTxStore struct {
	store.Store
	entered chan<- struct{}
	release <-chan struct{}
}

func (s gatedWithTxStore) WithTx(ctx context.Context, callback func(store.Store) error) error {
	s.entered <- struct{}{}
	<-s.release
	return s.Store.WithTx(ctx, callback)
}

func (s auditFailureStore) Audits() store.AuditStore {
	return appendFailureAuditStore{AuditStore: s.Store.Audits(), err: s.err}
}

func (s auditFailureStore) WithTx(ctx context.Context, callback func(store.Store) error) error {
	return s.Store.WithTx(ctx, func(txStore store.Store) error {
		return callback(auditFailureStore{Store: txStore, err: s.err})
	})
}

type appendFailureAuditStore struct {
	store.AuditStore
	err error
}

func (s appendFailureAuditStore) Append(context.Context, store.AuditLog) (store.AuditLog, error) {
	return store.AuditLog{}, s.err
}

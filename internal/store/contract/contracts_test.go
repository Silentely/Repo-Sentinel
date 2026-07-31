package contract_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
	"github.com/oklog/ulid/v2"
)

// ContractFactory 为不同数据库方言复用同一组 Store 行为契约。
type ContractFactory interface {
	Name() string
	Open(t *testing.T) store.Store
}

func TestSQLite管理员与SessionStore契约(t *testing.T) {
	runStoreContract(t, sqliteFactory{})
}

func TestPostgreSQL管理员与SessionStore契约(t *testing.T) {
	url := os.Getenv("REPOSENTINEL_TEST_POSTGRES_URL")
	if url == "" {
		t.Skip("未设置 REPOSENTINEL_TEST_POSTGRES_URL，跳过 PostgreSQL 运行时契约")
	}
	runStoreContract(t, postgresFactory{url: url})
}

// jsonRoundTripEqual 语义比较两份 JSON。
// SQLite 按原文存取可做字节级比较；PostgreSQL jsonb 有自己的规范文本格式
// （键重排、冒号后空格），往返后字节必然不同，只能按语义相等判定。
func jsonRoundTripEqual(a, b []byte) bool {
	if bytes.Equal(a, b) {
		return true
	}
	var av, bv any
	if json.Unmarshal(a, &av) != nil || json.Unmarshal(b, &bv) != nil {
		return false
	}
	return reflect.DeepEqual(av, bv)
}

type sqliteFactory struct{}

func (sqliteFactory) Name() string { return "SQLite" }

func (sqliteFactory) Open(t *testing.T) store.Store {
	t.Helper()
	// Atlas 直接接收 sql.DB 时使用 TMPDIR 下的固定 SQLite 锁名；每个契约测试必须隔离该锁。
	temporaryDir := t.TempDir()
	t.Setenv("TMPDIR", temporaryDir)
	databaseURL := "file:" + filepath.Join(temporaryDir, "contract.db")
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "sqlite",
		URL:          databaseURL,
		MaxOpenConns: 8,
		MaxIdleConns: 8,
	})
	if err != nil {
		t.Fatalf("打开 SQLite Store 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := opened.Close(); err != nil {
			t.Errorf("关闭 SQLite Store 失败: %v", err)
		}
	})
	return opened
}

type postgresFactory struct {
	url string
}

func (postgresFactory) Name() string { return "PostgreSQL" }

func (factory postgresFactory) Open(t *testing.T) store.Store {
	t.Helper()
	opened, err := store.Open(t.Context(), config.DatabaseConfig{
		Driver:       "postgres",
		URL:          factory.url,
		MaxOpenConns: 4,
		MaxIdleConns: 2,
	})
	if err != nil {
		t.Fatalf("打开 PostgreSQL Store 失败: %v", err)
	}
	t.Cleanup(func() {
		if err := opened.Admins().DeleteForTest(context.Background(), contractAdminID); err != nil && !errors.Is(err, store.ErrNotFound) {
			t.Errorf("清理 PostgreSQL 契约管理员失败: %v", err)
		}
		if err := opened.Close(); err != nil {
			t.Errorf("关闭 PostgreSQL Store 失败: %v", err)
		}
	})
	return opened
}

func runStoreContract(t *testing.T, factory ContractFactory) {
	t.Helper()
	// 此契约能捕获 GetOnly 缺失、空库未返回 ErrNotFound，或返回的并非唯一管理员。
	t.Run(factory.Name()+"/管理员GetOnly返回空库错误与唯一记录", func(t *testing.T) {
		opened := factory.Open(t)

		if _, err := opened.Admins().GetOnly(t.Context()); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("空库 GetOnly 错误=%v，期望 ErrNotFound", err)
		}

		created := createAdmin(t, opened, "Only Admin", "hash")
		found, err := opened.Admins().GetOnly(t.Context())
		if err != nil {
			t.Fatalf("GetOnly 查询唯一管理员失败: %v", err)
		}
		if found.ID != created.ID || found.Username != created.Username || found.PasswordHash != created.PasswordHash {
			t.Fatalf("GetOnly 管理员=%+v，期望 %+v", found, created)
		}
	})

	t.Run(factory.Name()+"/管理员唯一且查询会标准化用户名", func(t *testing.T) {
		opened := factory.Open(t)
		created := createAdmin(t, opened, "Repo Admin", "hash-v1")

		if created.Username != "Repo Admin" {
			t.Fatalf("展示用户名=%q，期望保留输入形式", created.Username)
		}
		found, err := opened.Admins().FindByUsername(t.Context(), "  repo ADMIN  ")
		if err != nil {
			t.Fatalf("按标准化用户名查询失败: %v", err)
		}
		if found.ID != created.ID || found.Username != created.Username {
			t.Fatalf("查询管理员=%+v，期望 %+v", found, created)
		}

		_, err = opened.Admins().Create(t.Context(), store.AdminAccount{
			ID:                "01K00000000000000000000002",
			Username:          "Another Admin",
			PasswordHash:      "hash-v2",
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
			PasswordChangedAt: time.Now(),
		})
		if !errors.Is(err, store.ErrConflict) {
			t.Fatalf("创建第二个管理员错误=%v，期望 ErrConflict", err)
		}
	})

	t.Run(factory.Name()+"/管理员密码更新时间统一为UTC", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "old-hash")
		changedAt := time.Date(2026, 7, 27, 11, 12, 13, 0, time.FixedZone("UTC+8", 8*60*60))

		updated, err := opened.Admins().UpdatePassword(t.Context(), admin.ID, "new-hash", changedAt)
		if err != nil {
			t.Fatalf("更新管理员密码失败: %v", err)
		}
		if updated.PasswordHash != "new-hash" {
			t.Fatalf("密码哈希=%q，期望 new-hash", updated.PasswordHash)
		}
		if updated.PasswordChangedAt.Location() != time.UTC || !updated.PasswordChangedAt.Equal(changedAt) {
			t.Fatalf("密码变更时间=%v，期望同一时刻的 UTC 时间", updated.PasswordChangedAt)
		}
	})

	// 此契约能捕获条件更新忽略旧哈希、失败时误改时间，或旧哈希可重复覆盖最新密码。
	t.Run(factory.Name()+"/管理员密码条件更新只接受当前哈希", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "old-hash")
		changedAt := time.Date(2026, 7, 27, 11, 12, 13, 0, time.FixedZone("UTC+8", 8*60*60))

		updated, err := opened.Admins().UpdatePasswordIfCurrent(
			t.Context(),
			admin.ID,
			"wrong-hash",
			"rejected-hash",
			changedAt,
		)
		if err != nil || updated {
			t.Fatalf("错误旧哈希条件更新结果=(%v, %v)，期望 false, nil", updated, err)
		}
		afterRejected, err := opened.Admins().Get(t.Context(), admin.ID)
		if err != nil {
			t.Fatalf("读取条件更新拒绝后的管理员失败: %v", err)
		}
		if afterRejected.PasswordHash != admin.PasswordHash ||
			!afterRejected.PasswordChangedAt.Equal(admin.PasswordChangedAt) ||
			!afterRejected.UpdatedAt.Equal(admin.UpdatedAt) {
			t.Fatal("错误旧哈希不应修改密码哈希或时间")
		}

		updated, err = opened.Admins().UpdatePasswordIfCurrent(
			t.Context(),
			admin.ID,
			admin.PasswordHash,
			"new-hash",
			changedAt,
		)
		if err != nil || !updated {
			t.Fatalf("正确旧哈希条件更新结果=(%v, %v)，期望 true, nil", updated, err)
		}
		afterUpdated, err := opened.Admins().Get(t.Context(), admin.ID)
		if err != nil {
			t.Fatalf("读取条件更新后的管理员失败: %v", err)
		}
		if afterUpdated.PasswordHash != "new-hash" ||
			afterUpdated.PasswordChangedAt.Location() != time.UTC ||
			!afterUpdated.PasswordChangedAt.Equal(changedAt) ||
			!afterUpdated.UpdatedAt.Equal(changedAt) {
			t.Fatal("正确旧哈希应持久化新密码哈希与 UTC 变更时间")
		}

		updated, err = opened.Admins().UpdatePasswordIfCurrent(
			t.Context(),
			admin.ID,
			admin.PasswordHash,
			"stale-overwrite-hash",
			changedAt.Add(time.Hour),
		)
		if err != nil || updated {
			t.Fatalf("重用旧哈希条件更新结果=(%v, %v)，期望 false, nil", updated, err)
		}
		afterReplay, err := opened.Admins().Get(t.Context(), admin.ID)
		if err != nil {
			t.Fatalf("读取旧哈希重放后的管理员失败: %v", err)
		}
		if afterReplay.PasswordHash != afterUpdated.PasswordHash ||
			!afterReplay.PasswordChangedAt.Equal(afterUpdated.PasswordChangedAt) ||
			!afterReplay.UpdatedAt.Equal(afterUpdated.UpdatedAt) {
			t.Fatal("旧哈希重放不应覆盖最新密码哈希或时间")
		}
	})

	t.Run(factory.Name()+"/Session令牌唯一且仅返回未过期记录", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "hash")
		now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
		active := newSession(admin.ID, "01K00000000000000000000011", "active-token", now.Add(time.Hour))
		if _, err := opened.Sessions().Create(t.Context(), active); err != nil {
			t.Fatalf("创建活动 Session 失败: %v", err)
		}

		duplicate := newSession(admin.ID, "01K00000000000000000000012", active.TokenHash, now.Add(2*time.Hour))
		if _, err := opened.Sessions().Create(t.Context(), duplicate); !errors.Is(err, store.ErrConflict) {
			t.Fatalf("重复 token_hash 错误=%v，期望 ErrConflict", err)
		}

		found, err := opened.Sessions().GetActiveByTokenHash(t.Context(), active.TokenHash, now)
		if err != nil || found.ID != active.ID {
			t.Fatalf("查询活动 Session=(%+v, %v)，期望 ID=%s", found, err, active.ID)
		}

		expired := newSession(admin.ID, "01K00000000000000000000013", "expired-token", now.Add(-time.Second))
		if _, err := opened.Sessions().Create(t.Context(), expired); err != nil {
			t.Fatalf("创建过期 Session 失败: %v", err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), expired.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("查询过期 Session 错误=%v，期望 ErrNotFound", err)
		}
	})

	t.Run(factory.Name()+"/Session撤销触碰清理与删除其他记录", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "hash")
		now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
		keep := newSession(admin.ID, "01K00000000000000000000021", "keep-token", now.Add(time.Hour))
		other := newSession(admin.ID, "01K00000000000000000000022", "other-token", now.Add(time.Hour))
		expired := newSession(admin.ID, "01K00000000000000000000023", "cleanup-token", now.Add(-time.Second))
		for _, session := range []store.AdminSession{keep, other, expired} {
			if _, err := opened.Sessions().Create(t.Context(), session); err != nil {
				t.Fatalf("创建 Session %s 失败: %v", session.ID, err)
			}
		}

		touchedAt := now.Add(10 * time.Minute)
		touched, err := opened.Sessions().Touch(t.Context(), keep.ID, touchedAt)
		if err != nil || !touched.LastSeenAt.Equal(touchedAt) || touched.LastSeenAt.Location() != time.UTC {
			t.Fatalf("Touch 结果=(%+v, %v)，期望 UTC 的 %v", touched, err, touchedAt)
		}
		deleted, err := opened.Sessions().DeleteOthers(t.Context(), admin.ID, keep.ID)
		if err != nil || deleted != 2 {
			t.Fatalf("DeleteOthers=(%d, %v)，期望删除 2 条", deleted, err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), other.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("其他 Session 仍可查询，错误=%v", err)
		}

		if err := opened.Sessions().Revoke(t.Context(), keep.ID); err != nil {
			t.Fatalf("撤销 Session 失败: %v", err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), keep.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("已撤销 Session 查询错误=%v，期望 ErrNotFound", err)
		}

		cleanup := newSession(admin.ID, "01K00000000000000000000024", "cleanup-only-token", now.Add(-time.Minute))
		if _, err := opened.Sessions().Create(t.Context(), cleanup); err != nil {
			t.Fatalf("创建待清理 Session 失败: %v", err)
		}
		cleaned, err := opened.Sessions().CleanupExpired(t.Context(), now)
		if err != nil || cleaned != 1 {
			t.Fatalf("CleanupExpired=(%d, %v)，期望清理 1 条", cleaned, err)
		}
	})

	// 此契约能捕获空 keepSessionID 被当成普通保留 ID，导致管理员 Session 未全部删除。
	t.Run(factory.Name()+"/Session空保留ID删除管理员全部记录", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "hash")
		now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.UTC)
		sessions := []store.AdminSession{
			newSession(admin.ID, "01K00000000000000000000025", "delete-all-token-1", now.Add(time.Hour)),
			newSession(admin.ID, "01K00000000000000000000026", "delete-all-token-2", now.Add(time.Hour)),
		}
		for _, session := range sessions {
			if _, err := opened.Sessions().Create(t.Context(), session); err != nil {
				t.Fatalf("创建待全部删除 Session %s 失败: %v", session.ID, err)
			}
		}

		deleted, err := opened.Sessions().DeleteOthers(t.Context(), admin.ID, "")
		if err != nil || deleted != len(sessions) {
			t.Fatalf("空保留 ID DeleteOthers=(%d, %v)，期望删除 %d 条", deleted, err, len(sessions))
		}
		for _, session := range sessions {
			if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
				t.Fatalf("Session %s 未被全部删除，错误=%v", session.ID, err)
			}
		}
	})

	t.Run(factory.Name()+"/测试删除管理员级联删除Session", func(t *testing.T) {
		opened := factory.Open(t)
		admin := createAdmin(t, opened, "Admin", "hash")
		now := time.Now().UTC()
		session := newSession(admin.ID, "01K00000000000000000000031", "cascade-token", now.Add(time.Hour))
		if _, err := opened.Sessions().Create(t.Context(), session); err != nil {
			t.Fatalf("创建 Session 失败: %v", err)
		}
		if err := opened.Admins().DeleteForTest(t.Context(), admin.ID); err != nil {
			t.Fatalf("测试清理管理员失败: %v", err)
		}
		if _, err := opened.Sessions().GetActiveByTokenHash(t.Context(), session.TokenHash, now); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("级联删除后查询 Session 错误=%v，期望 ErrNotFound", err)
		}
	})

	t.Run(factory.Name()+"/Setting按唯一Key更新且JSON往返", func(t *testing.T) {
		opened := factory.Open(t)
		firstJSON := json.RawMessage(`{"enabled":true,"targets":["a","b"]}`)
		first, err := opened.Settings().Upsert(t.Context(), store.SystemSetting{
			ID:        "01K00000000000000000000041",
			Key:       "notification.policy",
			ValueJSON: firstJSON,
			UpdatedAt: time.Now(),
			UpdatedBy: "01K00000000000000000000001",
		})
		if err != nil {
			t.Fatalf("首次写入 Setting 失败: %v", err)
		}
		if !jsonRoundTripEqual(first.ValueJSON, firstJSON) {
			t.Fatalf("首次 JSON=%s，期望 %s", first.ValueJSON, firstJSON)
		}

		secondJSON := json.RawMessage(`{"enabled":false}`)
		second, err := opened.Settings().Upsert(t.Context(), store.SystemSetting{
			ID:        "01K00000000000000000000042",
			Key:       first.Key,
			ValueJSON: secondJSON,
			UpdatedAt: time.Now().Add(time.Minute),
			UpdatedBy: "system",
		})
		if err != nil {
			t.Fatalf("更新 Setting 失败: %v", err)
		}
		if second.ID != first.ID {
			t.Fatalf("Upsert 后 ID=%s，期望保留 %s", second.ID, first.ID)
		}
		got, err := opened.Settings().Get(t.Context(), first.Key)
		if err != nil || !jsonRoundTripEqual(got.ValueJSON, secondJSON) {
			t.Fatalf("读取 Setting=(%s, %v)，期望 %s", got.ValueJSON, err, secondJSON)
		}
	})

	t.Run(factory.Name()+"/Audit仅追加并支持只读查询", func(t *testing.T) {
		opened := factory.Open(t)
		entry := store.AuditLog{
			ID:           ulid.Make().String(),
			Action:       "admin.login",
			ActorType:    "admin",
			ActorID:      "01K00000000000000000000001",
			TargetType:   "session",
			TargetID:     "01K00000000000000000000021",
			MetadataJSON: json.RawMessage(`{"result":"success"}`),
			IPAddress:    "127.0.0.1",
			CreatedAt:    time.Now(),
		}
		appended, err := opened.Audits().Append(t.Context(), entry)
		if err != nil {
			t.Fatalf("追加 Audit 失败: %v", err)
		}
		got, err := opened.Audits().Get(t.Context(), appended.ID)
		if err != nil || !jsonRoundTripEqual(got.MetadataJSON, entry.MetadataJSON) {
			t.Fatalf("读取 Audit=(%+v, %v)，期望 JSON=%s", got, err, entry.MetadataJSON)
		}
		listed, err := opened.Audits().List(t.Context(), 100, 0)
		if err != nil {
			t.Fatalf("列出 Audit 失败: %v", err)
		}
		found := false
		for _, candidate := range listed {
			found = found || candidate.ID == entry.ID
		}
		if !found {
			t.Fatalf("列出 Audit 未包含刚追加的记录 %s", entry.ID)
		}

		auditType := reflect.TypeOf((*store.AuditStore)(nil)).Elem()
		for _, forbidden := range []string{"Update", "Delete"} {
			if _, exists := auditType.MethodByName(forbidden); exists {
				t.Fatalf("AuditStore 不应暴露 %s", forbidden)
			}
		}
	})

	t.Run(factory.Name()+"/事务回调错误回滚全部写入", func(t *testing.T) {
		opened := factory.Open(t)
		rollbackErr := errors.New("rollback requested")
		err := opened.WithTx(t.Context(), func(txStore store.Store) error {
			createAdmin(t, txStore, "Transactional Admin", "hash")
			return rollbackErr
		})
		if !errors.Is(err, rollbackErr) {
			t.Fatalf("事务错误=%v，期望保留 callback 错误", err)
		}
		if _, err := opened.Admins().FindByUsername(t.Context(), "transactional admin"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("回滚后管理员查询错误=%v，期望 ErrNotFound", err)
		}
	})

	t.Run(factory.Name()+"/事务panic先回滚再原样抛出", func(t *testing.T) {
		opened := factory.Open(t)
		panicValue := &struct{ Message string }{Message: "boom"}
		func() {
			defer func() {
				if recovered := recover(); recovered != panicValue {
					t.Fatalf("recover=%#v，期望原 panic %#v", recovered, panicValue)
				}
			}()
			_ = opened.WithTx(t.Context(), func(txStore store.Store) error {
				createAdmin(t, txStore, "Panic Admin", "hash")
				panic(panicValue)
			})
		}()
		if _, err := opened.Admins().FindByUsername(t.Context(), "panic admin"); !errors.Is(err, store.ErrNotFound) {
			t.Fatalf("panic 回滚后管理员查询错误=%v，期望 ErrNotFound", err)
		}
	})
}

func createAdmin(t *testing.T, opened store.Store, username, passwordHash string) store.AdminAccount {
	t.Helper()
	now := time.Date(2026, 7, 27, 10, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60))
	admin, err := opened.Admins().Create(context.Background(), store.AdminAccount{
		ID:                contractAdminID,
		Username:          username,
		PasswordHash:      passwordHash,
		CreatedAt:         now,
		UpdatedAt:         now,
		PasswordChangedAt: now,
	})
	if err != nil {
		t.Fatalf("创建管理员失败: %v", err)
	}
	if admin.CreatedAt.Location() != time.UTC || admin.UpdatedAt.Location() != time.UTC {
		t.Fatalf("管理员时间未转换为 UTC: created=%v updated=%v", admin.CreatedAt, admin.UpdatedAt)
	}
	return admin
}

const contractAdminID = "01K00000000000000000000001"

func newSession(adminID, id, tokenHash string, expiresAt time.Time) store.AdminSession {
	createdAt := expiresAt.Add(-time.Hour)
	return store.AdminSession{
		ID:         id,
		AdminID:    adminID,
		TokenHash:  tokenHash,
		CSRFHash:   "csrf-" + tokenHash,
		CreatedAt:  createdAt,
		ExpiresAt:  expiresAt,
		LastSeenAt: createdAt,
		IPAddress:  "127.0.0.1",
		UserAgent:  "contract-test",
	}
}

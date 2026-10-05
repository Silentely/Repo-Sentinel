package migrations_test

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"ariga.io/atlas/sql/migrate"

	"github.com/Silentely/Repo-Sentinel/internal/config"
	"github.com/Silentely/Repo-Sentinel/internal/store"
)

// TestMigrationVersionsStrictlyMonotonic 校验每个方言下的迁移文件版本号严格单调递增，
// 防止 Atlas ExecOrderLinear 抛出 HistoryNonLinearError 阻断启动。
func TestMigrationVersionsStrictlyMonotonic(t *testing.T) {
	for _, dialect := range []string{"sqlite", "postgres"} {
		t.Run(dialect, func(t *testing.T) {
			dir, err := migrate.NewLocalDir(dialect)
			if err != nil {
				t.Fatalf("打开 %s Atlas 目录失败: %v", dialect, err)
			}
			files, err := dir.Files()
			if err != nil {
				t.Fatalf("读取 %s 迁移文件失败: %v", dialect, err)
			}

			var lastVersion int64 = -1
			var lastName string
			for _, file := range files {
				name := file.Name()
				parts := strings.SplitN(name, "_", 2)
				if len(parts) < 2 {
					t.Fatalf("无效的迁移文件名: %s", name)
				}
				v, err := strconv.ParseInt(parts[0], 10, 64)
				if err != nil {
					t.Fatalf("解析迁移版本号失败 %s: %v", parts[0], err)
				}
				if v <= lastVersion {
					t.Fatalf("%s 迁移版本号未保持严格单调递增: %s (版本 %d) <= %s (版本 %d)",
						dialect, name, v, lastName, lastVersion)
				}
				lastVersion = v
				lastName = name
			}
		})
	}
}

// TestDualEngineWebhookClaimFieldsMigration 在真实 SQLite（及在配置了环境变量时的 PostgreSQL）上
// 验证完整历史迁移与 webhook_deliveries claim 字段的读写一致性。
func TestDualEngineWebhookClaimFieldsMigration(t *testing.T) {
	runEngineTest := func(t *testing.T, driver string, getURL func(t *testing.T) string) {
		dbURL := getURL(t)
		opened, err := store.Open(t.Context(), config.DatabaseConfig{
			Driver:       driver,
			URL:          dbURL,
			MaxOpenConns: 4,
			MaxIdleConns: 2,
		})
		if err != nil {
			t.Fatalf("初始化 %s Store 失败: %v", driver, err)
		}
		defer func() {
			_ = opened.Close()
		}()

		deliveryStore := opened.WebhookDeliveries()
		deliveryID := "del-test-" + strconv.FormatInt(time.Now().UnixNano(), 10)
		now := time.Now().UTC().Truncate(time.Second)
		claimedUntil := now.Add(3 * time.Minute)

		created, err := deliveryStore.Create(t.Context(), store.WebhookDelivery{
			ID:                 "row-" + deliveryID,
			DeliveryID:         deliveryID,
			EventType:          "push",
			Status:             store.DeliveryProcessing,
			ReceivedAt:         now,
			ClaimToken:         "claim-ulid-12345",
			ClaimVersion:       1,
			ClaimedBy:          "worker-pod-1",
			ClaimedUntil:       &claimedUntil,
			AttemptCount:       1,
			LastErrorCode:      "",
			RepositoryFullName: "test/repo",
		})
		if err != nil {
			t.Fatalf("写入 WebhookDelivery 失败: %v", err)
		}

		// 查询验证
		fetched, err := deliveryStore.Get(t.Context(), created.ID)
		if err != nil {
			t.Fatalf("查询 WebhookDelivery 失败: %v", err)
		}

		if fetched.Status != store.DeliveryProcessing {
			t.Errorf("expected status %s, got %s", store.DeliveryProcessing, fetched.Status)
		}
		if fetched.ClaimToken != "claim-ulid-12345" {
			t.Errorf("expected claim_token claim-ulid-12345, got %s", fetched.ClaimToken)
		}
		if fetched.ClaimVersion != 1 {
			t.Errorf("expected claim_version 1, got %d", fetched.ClaimVersion)
		}
		if fetched.ClaimedBy != "worker-pod-1" {
			t.Errorf("expected claimed_by worker-pod-1, got %s", fetched.ClaimedBy)
		}
		if fetched.AttemptCount != 1 {
			t.Errorf("expected attempt_count 1, got %d", fetched.AttemptCount)
		}
		if fetched.ClaimedUntil == nil || !fetched.ClaimedUntil.Equal(claimedUntil) {
			t.Errorf("expected claimed_until %v, got %v", claimedUntil, fetched.ClaimedUntil)
		}

		// 验证 MarkProcessed 状态变更
		res, err := deliveryStore.MarkProcessed(t.Context(), fetched.ID, fetched.ClaimToken)
		if err != nil || !res.Applied || res.Stale {
			t.Fatalf("MarkProcessed 失败: res=%+v err=%v", res, err)
		}

		afterMark, err := deliveryStore.Get(t.Context(), created.ID)
		if err != nil {
			t.Fatalf("再次获取 WebhookDelivery 失败: %v", err)
		}
		if afterMark.Status != store.DeliveryProcessed {
			t.Errorf("expected status processed, got %s", afterMark.Status)
		}
		if afterMark.ProcessedAt == nil {
			t.Errorf("expected processed_at not nil")
		}
	}

	t.Run("SQLite", func(t *testing.T) {
		runEngineTest(t, "sqlite", func(t *testing.T) string {
			tmpDir := t.TempDir()
			t.Setenv("TMPDIR", tmpDir)
			return "file:" + filepath.Join(tmpDir, "migration_test.db")
		})
	})

	t.Run("PostgreSQL", func(t *testing.T) {
		pgURL := os.Getenv("REPOSENTINEL_TEST_POSTGRES_URL")
		if pgURL == "" {
			t.Skip("未设置 REPOSENTINEL_TEST_POSTGRES_URL，跳过 PostgreSQL 运行时迁移合约测试")
		}
		runEngineTest(t, "postgres", func(t *testing.T) string {
			return pgURL
		})
	})
}

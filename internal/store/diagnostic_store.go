package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	"github.com/Silentely/Repo-Sentinel/internal/store/ent/notificationoutbox"
)

type diagnosticStore struct {
	client     *entclient.Client
	driver     dialect.Driver
	driverName string
	rawURL     string
	settings   SettingsStore
	outbox     OutboxStore
}

func (d *diagnosticStore) getDB() *sql.DB {
	type hasDB interface{ DB() *sql.DB }
	if h, ok := d.driver.(hasDB); ok {
		return h.DB()
	}
	return nil
}

func (d *diagnosticStore) GetStorageDiagnostics(ctx context.Context) (StorageStats, error) {
	stats := StorageStats{
		Driver: d.driverName,
	}
	if stats.Driver == "" {
		if d.driver != nil && d.driver.Dialect() == dialect.Postgres {
			stats.Driver = "postgres"
		} else {
			stats.Driver = "sqlite"
		}
	}

	db := d.getDB()

	if stats.Driver == "sqlite" {
		path := strings.TrimPrefix(d.rawURL, "file:")
		if idx := strings.Index(path, "?"); idx != -1 {
			path = path[:idx]
		}
		path = strings.TrimSpace(path)
		if path != "" && path != ":memory:" {
			if fi, err := os.Stat(path); err == nil {
				stats.FileSizeBytes = fi.Size()
			}
			if fi, err := os.Stat(path + "-wal"); err == nil {
				stats.WALSizeBytes = fi.Size()
			}
		}

		if stats.FileSizeBytes == 0 && db != nil {
			var pageCount, pageSize int64
			_ = db.QueryRowContext(ctx, "PRAGMA page_count;").Scan(&pageCount)
			_ = db.QueryRowContext(ctx, "PRAGMA page_size;").Scan(&pageSize)
			if pageCount > 0 && pageSize > 0 {
				stats.FileSizeBytes = pageCount * pageSize
			}
		}
	} else if stats.Driver == "postgres" && db != nil {
		var dbSize int64
		_ = db.QueryRowContext(ctx, "SELECT pg_database_size(current_database());").Scan(&dbSize)
		stats.FileSizeBytes = dbSize
	}

	return stats, nil
}

func (d *diagnosticStore) GetOutboxDiagnostics(ctx context.Context) (OutboxStats, error) {
	var stats OutboxStats
	if d.client == nil {
		return stats, nil
	}

	pending, _ := d.client.NotificationOutbox.Query().
		Where(notificationoutbox.StatusIn("pending", "retry")).
		Count(ctx)
	stats.PendingCount = pending

	delivered, _ := d.client.NotificationOutbox.Query().
		Where(notificationoutbox.StatusEQ("delivered")).
		Count(ctx)
	stats.DeliveredCount = delivered

	dead, _ := d.client.NotificationOutbox.Query().
		Where(notificationoutbox.StatusEQ("dead")).
		Count(ctx)
	stats.DeadCount = dead

	return stats, nil
}

func (d *diagnosticStore) GetAIBudgetDiagnostics(ctx context.Context) (AIBudgetStats, error) {
	stats := AIBudgetStats{
		DailyTokenLimit: 100000,
		DailyCallLimit:  200,
	}

	if d.settings == nil {
		return stats, nil
	}

	todayKey := "ai_budget:" + time.Now().UTC().Format("2006-01-02")
	setting, err := d.settings.Get(ctx, todayKey)
	if err == nil && len(setting.ValueJSON) > 0 {
		var raw struct {
			Calls        int `json:"calls"`
			TokensEst    int `json:"tokens_est"`
			CostEstCents int `json:"cost_est_cents"`
			IsThrottled  any `json:"is_throttled"`
		}
		if err := json.Unmarshal(setting.ValueJSON, &raw); err == nil {
			stats.DailyTokensUsed = raw.TokensEst
			stats.DailyCallsUsed = raw.Calls
			stats.IsThrottled = parseBoolScan(raw.IsThrottled)
		}
	}

	return stats, nil
}

package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// sqliteMaintenanceDSN constructs a SQLite DSN specifically tuned for background maintenance:
// short busy timeout (1000ms) so it yields instantly if main connection holds a write lock,
// preventing stalls in web request serving.
func sqliteMaintenanceDSN(rawURL string) string {
	cleanURL := strings.TrimPrefix(rawURL, "file:")
	// Strip existing query parameters if present
	if idx := strings.Index(cleanURL, "?"); idx != -1 {
		cleanURL = cleanURL[:idx]
	}
	return fmt.Sprintf("file:%s?_pragma=busy_timeout(1000)&_pragma=journal_mode(WAL)&_pragma=synchronous(NORMAL)", cleanURL)
}

// OpenMaintenanceDB opens a dedicated SQLite connection with short busy timeout
// exclusively for non-blocking background maintenance tasks (e.g. WAL checkpoint, PRAGMA optimize).
func OpenMaintenanceDB(rawURL string) (*sql.DB, error) {
	db, err := sql.Open("sqlite", sqliteMaintenanceDSN(rawURL))
	if err != nil {
		return nil, fmt.Errorf("open sqlite maintenance db: %w", err)
	}
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	db.SetConnMaxLifetime(10 * time.Minute)
	return db, nil
}

// RunMaintenanceTask executes lightweight maintenance on SQLite WAL file:
// 1. Checks context cancellation
// 2. PRAGMA wal_checkpoint(PASSIVE) - merges completed WAL frames without waiting on readers/writers
// 3. PRAGMA optimize - updates query planner statistics
func RunMaintenanceTask(ctx context.Context, maintenanceDB *sql.DB) error {
	if maintenanceDB == nil {
		return nil
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	// 1. Run passive checkpoint (never blocks readers or writers)
	_, err := maintenanceDB.ExecContext(ctx, "PRAGMA wal_checkpoint(PASSIVE)")
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		// If busy or locked, return gracefully without failure
		return nil
	}

	// 2. Run query planner optimization
	_, _ = maintenanceDB.ExecContext(ctx, "PRAGMA optimize")

	return nil
}

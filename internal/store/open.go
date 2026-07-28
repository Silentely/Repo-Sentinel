package store

import (
	"context"
	"database/sql"
	"strings"
	"time"

	"entgo.io/ent/dialect"
	entsql "entgo.io/ent/dialect/sql"
	"github.com/Silentely/Repo-Sentinel/internal/config"
	entclient "github.com/Silentely/Repo-Sentinel/internal/store/ent"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

const databasePingTimeout = 5 * time.Second

type storeImpl struct {
	client  *entclient.Client
	closeFn func() error
}

// Open 打开数据库、完成安全连通性检查并应用版本化迁移。
func Open(ctx context.Context, cfg config.DatabaseConfig) (Store, error) {
	db, entDialect, migrationDialect, err := openDatabase(cfg)
	if err != nil {
		return nil, errDatabaseUnavailable
	}

	pingCtx, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, errDatabaseUnavailable
	}
	if err := applyMigrations(ctx, db, migrationDialect); err != nil {
		_ = db.Close()
		return nil, errMigrationFailed
	}

	driver := entsql.OpenDB(entDialect, db)
	return newStore(entclient.NewClient(entclient.Driver(driver)), db.Close), nil
}

func openDatabase(cfg config.DatabaseConfig) (*sql.DB, string, string, error) {
	switch cfg.Driver {
	case "sqlite":
		db, err := sql.Open("sqlite", sqliteDSN(cfg.URL))
		if err != nil {
			return nil, "", "", err
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return db, dialect.SQLite, "sqlite", nil
	case "postgres":
		db, err := sql.Open("pgx", cfg.URL)
		if err != nil {
			return nil, "", "", err
		}
		db.SetMaxOpenConns(cfg.MaxOpenConns)
		db.SetMaxIdleConns(cfg.MaxIdleConns)
		return db, dialect.Postgres, "postgres", nil
	default:
		return nil, "", "", errDatabaseUnavailable
	}
}

func sqliteDSN(rawURL string) string {
	separator := "?"
	if strings.Contains(rawURL, "?") {
		separator = "&"
	}
	return rawURL + separator + strings.Join([]string{
		"_pragma=foreign_keys(1)",
		"_pragma=journal_mode(WAL)",
		"_pragma=busy_timeout(5000)",
		"_pragma=synchronous(NORMAL)",
	}, "&")
}

func newStore(client *entclient.Client, closeFn func() error) *storeImpl {
	return &storeImpl{client: client, closeFn: closeFn}
}

func (s *storeImpl) Admins() AdminStore                 { return &adminStore{client: s.client} }
func (s *storeImpl) Sessions() SessionStore             { return &sessionStore{client: s.client} }
func (s *storeImpl) Settings() SettingsStore            { return &settingsStore{client: s.client} }
func (s *storeImpl) Audits() AuditStore                 { return &auditStore{client: s.client} }
func (s *storeImpl) Installations() InstallationStore   { return &installationStore{client: s.client} }
func (s *storeImpl) Repositories() RepositoryStore      { return &repositoryStore{client: s.client} }
func (s *storeImpl) WebhookDeliveries() WebhookDeliveryStore {
	return &webhookDeliveryStore{client: s.client}
}
func (s *storeImpl) WorkItems() WorkItemStore           { return &workItemStore{client: s.client} }
func (s *storeImpl) WorkflowRuns() WorkflowRunStore     { return &workflowRunStore{client: s.client} }
func (s *storeImpl) SecurityAlerts() SecurityAlertStore { return &securityAlertStore{client: s.client} }
func (s *storeImpl) Events() EventStore                 { return &eventStore{client: s.client} }
func (s *storeImpl) Channels() ChannelStore             { return &channelStore{client: s.client} }
func (s *storeImpl) Outbox() OutboxStore                { return &outboxStore{client: s.client} }
func (s *storeImpl) Cursors() CursorStore               { return &cursorStore{client: s.client} }
func (s *storeImpl) Close() error                       { return s.closeFn() }

package store

import (
	"context"
	"database/sql"
	"fmt"
	"net"
	"net/url"
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
		return nil, err
	}

	pingCtx, cancel := context.WithTimeout(ctx, databasePingTimeout)
	defer cancel()
	if err := db.PingContext(pingCtx); err != nil {
		_ = db.Close()
		return nil, classifyConnectError(cfg.Driver, err)
	}
	if err := applyMigrations(ctx, db, migrationDialect, cfg.URL); err != nil {
		_ = db.Close()
		return nil, classifyMigrationError(err)
	}

	driver := entsql.OpenDB(entDialect, db)
	return newStore(entclient.NewClient(entclient.Driver(driver)), db.Close), nil
}

func openDatabase(cfg config.DatabaseConfig) (*sql.DB, string, string, error) {
	switch cfg.Driver {
	case "sqlite":
		db, err := sql.Open("sqlite", sqliteDSN(cfg.URL))
		if err != nil {
			return nil, "", "", newOpenError("dsn", "SQLite DSN 无效", err)
		}
		db.SetMaxOpenConns(1)
		db.SetMaxIdleConns(1)
		return db, dialect.SQLite, "sqlite", nil
	case "postgres":
		if reason := validatePostgresURL(cfg.URL); reason != "" {
			return nil, "", "", newOpenError("dsn", reason, errDatabaseUnavailable)
		}
		db, err := sql.Open("pgx", cfg.URL)
		if err != nil {
			return nil, "", "", newOpenError("dsn", "PostgreSQL 连接串无法解析（请 URL 编码密码中的 # @ : / ? 等字符）", err)
		}
		db.SetMaxOpenConns(cfg.MaxOpenConns)
		db.SetMaxIdleConns(cfg.MaxIdleConns)
		return db, dialect.Postgres, "postgres", nil
	default:
		return nil, "", "", newOpenError("driver", "数据库驱动须为 sqlite 或 postgres", errDatabaseUnavailable)
	}
}

// validatePostgresURL 在真正拨号前做安全检查，返回不含凭据的中文原因；通过则返回空串。
func validatePostgresURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "PostgreSQL 连接串为空"
	}
	// 字面量 # 在 URL 中会截断（fragment）或导致 invalid port；密码必须写成 %23。
	if strings.Contains(raw, "#") {
		return "连接串含未编码的 #（密码等特殊字符须 URL 编码：#→%23 (→%28 )→%29 @→%40 :→%3A）"
	}
	u, err := url.Parse(raw)
	if err != nil {
		return "PostgreSQL 连接串格式无效（密码中的 # @ : / ? ( ) 等须百分号编码）"
	}
	scheme := strings.ToLower(u.Scheme)
	if scheme != "postgres" && scheme != "postgresql" {
		return "连接串 scheme 须为 postgres 或 postgresql"
	}
	if u.Host == "" {
		return "连接串缺少主机（host:port）"
	}
	if host, port, err := net.SplitHostPort(u.Host); err == nil {
		if host == "" || port == "" {
			return "连接串主机或端口无效"
		}
	}
	return ""
}

func classifyMigrationError(err error) error {
	if err == nil {
		return fmt.Errorf("%w", errMigrationFailed)
	}
	msg := strings.ToLower(err.Error())
	reason := "数据库迁移失败"
	switch {
	case strings.Contains(msg, "not clean") || strings.Contains(msg, "allow-dirty") ||
		strings.Contains(msg, "baseline version"):
		reason = "数据库非空且无迁移基线（请用空业务库或升级含 allow-dirty 的版本）"
	case strings.Contains(msg, "set_config") || strings.Contains(msg, "search_path"):
		reason = "数据库禁止修改 search_path（部分托管 PG）；请升级含兼容修复的版本"
	case strings.Contains(msg, "permission denied") || strings.Contains(msg, "42501"):
		reason = "数据库权限不足（需对 public 有 CREATE，或使用可建表的角色）"
	case strings.Contains(msg, "datetime") && strings.Contains(msg, "does not exist"):
		reason = "迁移 SQL 含 SQLite 类型 datetime，Postgres 需 timestamptz"
	case strings.Contains(msg, "newer than supported"):
		reason = "数据库 revision 高于当前程序，禁止降级"
	case strings.Contains(msg, "checksum") || strings.Contains(msg, "hash"):
		reason = "迁移校验和与目录不一致"
	}
	// 公开文案不含连接串；底层 err 仍可通过 errors.Unwrap 链排查。
	return fmt.Errorf("%w: %s", errMigrationFailed, reason)
}

func classifyConnectError(driver string, err error) error {
	if err == nil {
		return newOpenError("unknown", "无法打开数据库", errDatabaseUnavailable)
	}
	msg := strings.ToLower(err.Error())
	switch {
	case strings.Contains(msg, "timeout") || strings.Contains(msg, "deadline exceeded") ||
		strings.Contains(msg, "i/o timeout"):
		return newOpenError("connect", "连接数据库超时（检查主机、端口、同项目内网与安全组）", err)
	case strings.Contains(msg, "no such host") || strings.Contains(msg, "server misbehaving") ||
		strings.Contains(msg, "name resolution"):
		return newOpenError("connect", "无法解析数据库主机名（检查 Northflank 内部 DNS / 服务名）", err)
	case strings.Contains(msg, "connection refused") || strings.Contains(msg, "connect: connection refused"):
		return newOpenError("connect", "数据库拒绝连接（检查端口与进程是否就绪）", err)
	case strings.Contains(msg, "network is unreachable") || strings.Contains(msg, "no route to host"):
		return newOpenError("connect", "网络不可达（应用与数据库是否在同一私有网络）", err)
	case strings.Contains(msg, "password authentication failed") || strings.Contains(msg, "auth failed") ||
		strings.Contains(msg, "28p01"):
		return newOpenError("connect", "数据库认证失败（用户名或密码错误；特殊字符须 URL 编码）", err)
	case strings.Contains(msg, "does not exist") && strings.Contains(msg, "database"):
		return newOpenError("connect", "目标数据库不存在（请先创建库或改 URL 中的库名）", err)
	case strings.Contains(msg, "ssl") || strings.Contains(msg, "tls"):
		return newOpenError("connect", "TLS/SSL 握手失败（内网可试 sslmode=disable，公网用 require）", err)
	case strings.Contains(msg, "invalid port") || strings.Contains(msg, "parse") && strings.Contains(msg, "postgres"):
		return newOpenError("dsn", "连接串解析失败（密码特殊字符须 URL 编码）", err)
	case driver == "sqlite" && (strings.Contains(msg, "unable to open") || strings.Contains(msg, "readonly") ||
		strings.Contains(msg, "permission denied")):
		return newOpenError("connect", "无法写入 SQLite 文件（检查路径与卷挂载权限）", err)
	default:
		return newOpenError("connect", "无法连通数据库（网络、认证或 DSN）", err)
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

func (s *storeImpl) Admins() AdminStore               { return &adminStore{client: s.client} }
func (s *storeImpl) Sessions() SessionStore           { return &sessionStore{client: s.client} }
func (s *storeImpl) Settings() SettingsStore          { return &settingsStore{client: s.client} }
func (s *storeImpl) Audits() AuditStore               { return &auditStore{client: s.client} }
func (s *storeImpl) Installations() InstallationStore { return &installationStore{client: s.client} }
func (s *storeImpl) Repositories() RepositoryStore    { return &repositoryStore{client: s.client} }
func (s *storeImpl) WebhookDeliveries() WebhookDeliveryStore {
	return &webhookDeliveryStore{client: s.client}
}
func (s *storeImpl) WorkItems() WorkItemStore           { return &workItemStore{client: s.client} }
func (s *storeImpl) WorkflowRuns() WorkflowRunStore     { return &workflowRunStore{client: s.client} }
func (s *storeImpl) SecurityAlerts() SecurityAlertStore { return &securityAlertStore{client: s.client} }
func (s *storeImpl) Events() EventStore                 { return &eventStore{client: s.client} }

// RepoStatSnapshots 返回仓库指标快照存储访问器。
// 具体实现随 stargazer 同步任务落地（Ent 实体 + 迁移），本阶段仅声明接口。
func (s *storeImpl) RepoStatSnapshots() RepoStatSnapshotStore { return nil }
func (s *storeImpl) Channels() ChannelStore                   { return &channelStore{client: s.client} }
func (s *storeImpl) Outbox() OutboxStore                      { return &outboxStore{client: s.client} }
func (s *storeImpl) Cursors() CursorStore                     { return &cursorStore{client: s.client} }
func (s *storeImpl) Close() error                             { return s.closeFn() }

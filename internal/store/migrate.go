package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"sync/atomic"
	"time"

	"ariga.io/atlas/sql/migrate"
	atlaspostgres "ariga.io/atlas/sql/postgres"
	atlassqlite "ariga.io/atlas/sql/sqlite"
	migrationfiles "github.com/Silentely/Repo-Sentinel/migrations"
)

const (
	revisionTableName = "atlas_schema_revisions"
	migrationLockName = "reposentinel_atlas_migrate"
)

// sqlite 驱动的 Lock 在进程内按名称互斥；并行单测会打开不同库文件却争用同一锁名。
// 用序号隔离 SQLite 迁移锁；PostgreSQL 仍用固定名（库级 advisory lock）。
var sqliteMigrationLockSeq atomic.Uint64

func applyMigrations(ctx context.Context, db *sql.DB, dialectName string) (err error) {
	dir, err := embeddedMigrationDir(dialectName)
	if err != nil {
		return err
	}
	if err := migrate.Validate(dir); err != nil {
		return err
	}
	driver, err := atlasDriver(db, dialectName)
	if err != nil {
		return err
	}
	lockName := migrationLockName
	if dialectName == "sqlite" {
		lockName = fmt.Sprintf("%s_%d", migrationLockName, sqliteMigrationLockSeq.Add(1))
	}
	unlock, err := driver.Lock(ctx, lockName, 30*time.Second)
	if err != nil {
		return err
	}
	defer func() {
		if unlockErr := unlock(); err == nil && unlockErr != nil {
			err = unlockErr
		}
	}()

	revisions := &revisionStore{db: db, dialect: dialectName}
	if err := revisions.init(ctx); err != nil {
		return err
	}
	if err := rejectUnsupportedRevision(ctx, revisions, dir); err != nil {
		return err
	}
	executor, err := migrate.NewExecutor(
		driver,
		dir,
		revisions,
		migrate.WithExecOrder(migrate.ExecOrderLinear),
		migrate.WithOperatorVersion("reposentinel"),
	)
	if err != nil {
		return err
	}
	if err := executor.ExecuteN(ctx, 0); err != nil && !errors.Is(err, migrate.ErrNoPendingFiles) {
		return err
	}
	return nil
}

func embeddedMigrationDir(dialectName string) (migrate.Dir, error) {
	source, err := migrationfiles.Dialect(dialectName)
	if err != nil {
		return nil, err
	}
	entries, err := fs.ReadDir(source, ".")
	if err != nil {
		return nil, err
	}
	dir := &migrate.MemDir{}
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		content, err := fs.ReadFile(source, entry.Name())
		if err != nil {
			return nil, err
		}
		if err := dir.WriteFile(entry.Name(), content); err != nil {
			return nil, err
		}
	}
	return dir, nil
}

func atlasDriver(db *sql.DB, dialectName string) (migrate.Driver, error) {
	switch dialectName {
	case "sqlite":
		return atlassqlite.Open(db)
	case "postgres":
		return atlaspostgres.Open(db)
	default:
		return nil, errors.New("unsupported migration dialect")
	}
}

func rejectUnsupportedRevision(ctx context.Context, revisions *revisionStore, dir migrate.Dir) error {
	applied, err := revisions.ReadRevisions(ctx)
	if err != nil {
		return err
	}
	if len(applied) == 0 {
		return nil
	}
	files, err := dir.Files()
	if err != nil {
		return err
	}
	if len(files) == 0 || applied[len(applied)-1].Version > files[len(files)-1].Version() {
		return errors.New("database revision is newer than supported migrations")
	}
	return nil
}

type revisionStore struct {
	db      *sql.DB
	dialect string
}

func (r *revisionStore) Ident() *migrate.TableIdent {
	return &migrate.TableIdent{Name: revisionTableName}
}

func (r *revisionStore) init(ctx context.Context) error {
	query := `CREATE TABLE IF NOT EXISTS atlas_schema_revisions (
		version text NOT NULL PRIMARY KEY,
		description text NOT NULL,
		type integer NOT NULL,
		applied integer NOT NULL,
		total integer NOT NULL,
		executed_at datetime NOT NULL,
		execution_time integer NOT NULL,
		error text NOT NULL DEFAULT '',
		error_stmt text NOT NULL DEFAULT '',
		hash text NOT NULL,
		partial_hashes text NOT NULL DEFAULT '[]',
		operator_version text NOT NULL DEFAULT ''
	)`
	if r.dialect == "postgres" {
		query = `CREATE TABLE IF NOT EXISTS atlas_schema_revisions (
			version character varying(255) NOT NULL PRIMARY KEY,
			description character varying(255) NOT NULL,
			type bigint NOT NULL,
			applied bigint NOT NULL,
			total bigint NOT NULL,
			executed_at timestamptz NOT NULL,
			execution_time bigint NOT NULL,
			error text NOT NULL DEFAULT '',
			error_stmt text NOT NULL DEFAULT '',
			hash character varying(255) NOT NULL,
			partial_hashes jsonb NOT NULL DEFAULT '[]'::jsonb,
			operator_version character varying(255) NOT NULL DEFAULT ''
		)`
	}
	_, err := r.db.ExecContext(ctx, query)
	return err
}

func (r *revisionStore) ReadRevisions(ctx context.Context) ([]*migrate.Revision, error) {
	rows, err := r.db.QueryContext(ctx, `SELECT version, description, type, applied, total,
		executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version
		FROM atlas_schema_revisions ORDER BY version`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var revisions []*migrate.Revision
	for rows.Next() {
		revision, err := scanRevision(rows)
		if err != nil {
			return nil, err
		}
		revisions = append(revisions, revision)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return revisions, nil
}

func (r *revisionStore) ReadRevision(ctx context.Context, version string) (*migrate.Revision, error) {
	placeholder := "?"
	if r.dialect == "postgres" {
		placeholder = "$1"
	}
	row := r.db.QueryRowContext(ctx, `SELECT version, description, type, applied, total,
		executed_at, execution_time, error, error_stmt, hash, partial_hashes, operator_version
		FROM atlas_schema_revisions WHERE version = `+placeholder, version)
	revision, err := scanRevision(row)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, migrate.ErrRevisionNotExist
	}
	return revision, err
}

func (r *revisionStore) WriteRevision(ctx context.Context, revision *migrate.Revision) error {
	partialHashes, err := json.Marshal(revision.PartialHashes)
	if err != nil {
		return err
	}
	args := []any{
		revision.Version,
		revision.Description,
		int64(revision.Type),
		revision.Applied,
		revision.Total,
		revision.ExecutedAt.UTC(),
		int64(revision.ExecutionTime),
		revision.Error,
		revision.ErrorStmt,
		revision.Hash,
		string(partialHashes),
		revision.OperatorVersion,
	}
	query := `INSERT INTO atlas_schema_revisions
		(version, description, type, applied, total, executed_at, execution_time,
		 error, error_stmt, hash, partial_hashes, operator_version)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(version) DO UPDATE SET
		 description=excluded.description, type=excluded.type, applied=excluded.applied,
		 total=excluded.total, executed_at=excluded.executed_at,
		 execution_time=excluded.execution_time, error=excluded.error,
		 error_stmt=excluded.error_stmt, hash=excluded.hash,
		 partial_hashes=excluded.partial_hashes, operator_version=excluded.operator_version`
	if r.dialect == "postgres" {
		query = `INSERT INTO atlas_schema_revisions
			(version, description, type, applied, total, executed_at, execution_time,
			 error, error_stmt, hash, partial_hashes, operator_version)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11::jsonb, $12)
			ON CONFLICT(version) DO UPDATE SET
			 description=excluded.description, type=excluded.type, applied=excluded.applied,
			 total=excluded.total, executed_at=excluded.executed_at,
			 execution_time=excluded.execution_time, error=excluded.error,
			 error_stmt=excluded.error_stmt, hash=excluded.hash,
			 partial_hashes=excluded.partial_hashes, operator_version=excluded.operator_version`
	}
	_, err = r.db.ExecContext(ctx, query, args...)
	return err
}

func (r *revisionStore) DeleteRevision(ctx context.Context, version string) error {
	query := `DELETE FROM atlas_schema_revisions WHERE version = ?`
	if r.dialect == "postgres" {
		query = `DELETE FROM atlas_schema_revisions WHERE version = $1`
	}
	_, err := r.db.ExecContext(ctx, query, version)
	return err
}

type rowScanner interface {
	Scan(...any) error
}

func scanRevision(scanner rowScanner) (*migrate.Revision, error) {
	var (
		revision      migrate.Revision
		revisionType  int64
		executionTime int64
		partialHashes string
	)
	if err := scanner.Scan(
		&revision.Version,
		&revision.Description,
		&revisionType,
		&revision.Applied,
		&revision.Total,
		&revision.ExecutedAt,
		&executionTime,
		&revision.Error,
		&revision.ErrorStmt,
		&revision.Hash,
		&partialHashes,
		&revision.OperatorVersion,
	); err != nil {
		return nil, err
	}
	revision.Type = migrate.RevisionType(revisionType)
	revision.ExecutionTime = time.Duration(executionTime)
	if err := json.Unmarshal([]byte(partialHashes), &revision.PartialHashes); err != nil {
		return nil, err
	}
	revision.ExecutedAt = revision.ExecutedAt.UTC()
	return &revision, nil
}

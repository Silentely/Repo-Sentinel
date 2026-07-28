# 配置 RepoSentinel

> **已迁移**：请阅读 [配置参考](/reference/configuration)（完整环境变量、SQLite / PostgreSQL）。  
> 本路径仅兼容旧链接；仓库内权威正文在 `docs/reference/configuration.md`。

在 GitHub 上请打开：

https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/configuration.md

启动新实例时设置 `REPOSENTINEL_ENCRYPTION_KEY`。轮换时，把旧值放入 `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS`，把新值放入当前变量；确认实例完成迁移后，再按组织的 Secret 保留策略移除旧值。不要把这两个变量写入 YAML、shell 历史或日志。

## 选择 SQLite 或 PostgreSQL

### SQLite

SQLite 适合单实例和本地部署。应用会自动应用迁移，并使用单打开连接、WAL 和 busy timeout：

```bash
REPOSENTINEL_DATABASE_DRIVER="sqlite" \
REPOSENTINEL_DATABASE_URL="file:./.tmp/reposentinel.db" \
".tmp/reposentinel" serve
```

### PostgreSQL

PostgreSQL 必须提供非空连接 URL；未显式设置连接池时，默认最大打开连接数为 10。测试 compose 文件使用 PostgreSQL 17：

```bash
docker compose -f "deployments/test/postgres.compose.yml" up -d
REPOSENTINEL_DATABASE_DRIVER="postgres" \
REPOSENTINEL_DATABASE_URL="postgres://reposentinel:reposentinel@127.0.0.1:55432/reposentinel_test?sslmode=disable" \
".tmp/reposentinel" serve
```

生产环境请使用 Secret 管理器提供连接 URL，并限制数据库网络访问。当前开发环境如果没有 Docker、`psql` 或 PostgreSQL URL，只记录 `SKIP`，不把 SQLite 结果写成 PostgreSQL 通过。

## 备份和恢复

### 备份 SQLite

优先使用 SQLite CLI 的 `.backup`，它调用 Online Backup API，可以生成一致的独立数据库文件：

```bash
mkdir -p ".tmp/backups"
sqlite3 ".tmp/reposentinel.db" ".backup '.tmp/backups/reposentinel-YYYYMMDD.db'"
```

不要直接复制仍在运行的 WAL 数据库主文件。如果本机没有 `sqlite3`，先干净停止服务，再使用能把数据库、`-wal` 和 `-shm` 作为一致集合处理的存储快照工具。

恢复时停止服务，先另存当前数据库，再把备份文件复制回配置中的路径，然后启动服务。启动时会校验迁移版本和加密探针；恢复数据库时必须同时提供匹配的当前/上一把主密钥。

### 备份 PostgreSQL

使用 PostgreSQL 官方工具生成 custom 格式备份：

```bash
pg_dump --format=custom --file="reposentinel-YYYYMMDD.dump" "$REPOSENTINEL_DATABASE_URL"
```

恢复前确认目标库和维护窗口，再使用 `pg_restore`。`--clean` 会删除目标库中的对象，请先确认目标连接和备份文件：

```bash
pg_restore --clean --if-exists --dbname="$REPOSENTINEL_DATABASE_URL" "reposentinel-YYYYMMDD.dump"
```

迁移由应用启动流程统一执行；不要直接修改业务表或 Atlas revision 表。

# 配置 RepoSentinel

先确定配置来源，再启动服务。RepoSentinel 按“默认值 → YAML 文件 → 环境变量”的顺序合并配置；环境变量优先级最高。

## 使用 YAML 配置基础值

仓库提供了 [示例配置](../../configs/reposentinel.example.yaml)。服务命令、配置校验和管理员密码恢复都支持 `--config`：

```bash
".tmp/reposentinel" config validate --config "configs/reposentinel.example.yaml"
".tmp/reposentinel" serve --config "configs/reposentinel.example.yaml"
```

YAML 适合保存地址、连接池和开关等非敏感值。密码、主密钥、Webhook Secret、PAT 和 Telegram Token 只从 Secret 管理器或环境变量注入。

## 使用环境变量覆盖配置

### HTTP、数据库和管理员

| 变量 | 用途 |
| --- | --- |
| `REPOSENTINEL_HTTP_ADDR` | 监听地址，必须包含主机和端口，例如 `127.0.0.1:8080`。 |
| `REPOSENTINEL_PUBLIC_BASE_URL` | 对外访问地址；生产环境使用 HTTPS，本机 loopback HTTP 可用于开发。 |
| `REPOSENTINEL_DATABASE_DRIVER` | `sqlite` 或 `postgres`。 |
| `REPOSENTINEL_DATABASE_URL` | SQLite 文件 DSN 或 PostgreSQL 连接 URL。 |
| `REPOSENTINEL_DATABASE_MAX_OPEN_CONNS` | PostgreSQL 最大打开连接数；SQLite 固定使用单连接。 |
| `REPOSENTINEL_DATABASE_MAX_IDLE_CONNS` | PostgreSQL 最大空闲连接数。 |
| `REPOSENTINEL_ADMIN_USERNAME` | 首次启动时创建唯一管理员的用户名。必须和密码成对提供。 |
| `REPOSENTINEL_ADMIN_PASSWORD` | 首次启动时创建唯一管理员的密码；已有管理员时不会覆盖密码。 |
| `REPOSENTINEL_ADMIN_SESSION_TTL` | Session 有效期，使用 Go duration，例如 `24h`。 |
| `REPOSENTINEL_SETUP_ALLOW_REMOTE` | 是否允许来源 IP 或请求 Host 非 loopback 的首次 setup，默认 `false`。 |

### 主密钥

| 变量 | 用途 |
| --- | --- |
| `REPOSENTINEL_ENCRYPTION_KEY` | 当前数据加密主密钥。解码后必须正好 32 字节。 |
| `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS` | 密钥轮换期间保留的上一把主密钥。 |

### GitHub 和通知（Phase 1 仅保存配置边界）

| 变量 | 用途 |
| --- | --- |
| `REPOSENTINEL_GITHUB_APP_ID` | GitHub App 数字 ID。 |
| `REPOSENTINEL_GITHUB_CLIENT_ID` | GitHub App Client ID。 |
| `REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH` | GitHub App 私钥文件路径。 |
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | 当前 Webhook Secret。 |
| `REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET` | 轮换期间的上一把 Webhook Secret。 |
| `REPOSENTINEL_EXTERNAL_PAT` | 外部仓库访问 PAT。 |
| `REPOSENTINEL_TELEGRAM_TOKEN` | Telegram Bot Token。 |
| `REPOSENTINEL_TELEGRAM_CHAT_ID` | Telegram 目标 Chat ID。 |
| `REPOSENTINEL_HTTP_WEBHOOK_URL` | 通用 HTTP Webhook 地址。 |
| `REPOSENTINEL_HTTP_WEBHOOK_SECRET` | 通用 HTTP Webhook 签名 Secret。 |
| `REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE` | 是否允许通知目标为私网地址，默认 `false`。 |

### 日志

| 变量 | 用途 |
| --- | --- |
| `REPOSENTINEL_LOG_FORMAT` | `json` 或 `text`。 |
| `REPOSENTINEL_LOG_LEVEL` | `debug`、`info`、`warn` 或 `error`。 |
| `LOG_FORMAT` | 日志格式的标准别名；同时存在时覆盖 `REPOSENTINEL_LOG_FORMAT`。 |
| `LOG_LEVEL` | 日志级别的标准别名；同时存在时覆盖 `REPOSENTINEL_LOG_LEVEL`。 |

配置错误会返回稳定的 `validation_failed`，错误信息不会回显密码、连接 URL 或主密钥原文。`config validate` 只输出安全摘要：

```bash
".tmp/reposentinel" config validate
```

## 生成和轮换 32 字节主密钥

主密钥可以使用 base64 或 hex 表示。先在 Secret 管理器中生成并保存值，再注入进程：

```bash
openssl rand -base64 32
openssl rand -hex 32
```

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

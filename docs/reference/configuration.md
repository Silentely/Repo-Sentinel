# 配置参考

配置合并顺序：**默认值 → YAML 文件 → 环境变量**（环境变量优先）。  
Secret（密码、主密钥、Webhook Secret、Token）只放环境变量或密钥管理，不要写入仓库内 YAML。

相关：[快速开始](/guide/quick-start) · [Docker 部署](/deploy/docker) · [运维手册](/reference/ops)

## 最小可运行示例

### SQLite（默认，单机 / Docker 最常见）

```bash
REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
REPOSENTINEL_PUBLIC_BASE_URL=https://monitor.example.com \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:/data/reposentinel.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
REPOSENTINEL_GITHUB_WEBHOOK_SECRET=your-webhook-secret \
reposentinel serve
```

### PostgreSQL

```bash
REPOSENTINEL_DATABASE_DRIVER=postgres \
REPOSENTINEL_DATABASE_URL="postgres://user:pass@db:5432/reposentinel?sslmode=require" \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
REPOSENTINEL_GITHUB_WEBHOOK_SECRET=your-webhook-secret \
reposentinel serve
```

- `driver` 仅支持 `sqlite` / `postgres`
- Postgres **必须**提供非空 URL；库先建好，启动时自动迁移
- Docker Compose 默认 SQLite 见根目录 `docker-compose.yml`；改 Postgres 时覆盖上述两个变量即可

#### 连接串密码必须 URL 编码

`REPOSENTINEL_DATABASE_URL` 是标准 URL。密码里的 `#`、`@`、`:`、`/`、`?`、`(`、`)`、`%` 等**不能原样粘贴**，否则解析失败，启动报 `database_unavailable`。

| 字符 | 编码 |
|------|------|
| `#` | `%23` |
| `@` | `%40` |
| `:` | `%3A` |
| `/` | `%2F` |
| `?` | `%3F` |
| `(` | `%28` |
| `)` | `%29` |
| `%` | `%25` |
| 空格 | `%20` |

示例：密码为 `9t#dE(4#0g` 时：

```bash
# 错误（# 会截断 URL）
# postgres://postgres:9t#dE(4#0g@db:5432/postgres?sslmode=disable

# 正确
REPOSENTINEL_DATABASE_URL="postgres://postgres:9t%23dE%284%230g@db:5432/postgres?sslmode=disable"
```

编码命令：

```bash
python3 -c 'import urllib.parse; print(urllib.parse.quote("你的密码", safe=""))'
```

平台（如 Northflank）若提供「连接 URL」，优先复制官方已编码的 URL；内网库常用 `sslmode=disable`，公网用 `sslmode=require`。

校验（不回显 Secret）：

```bash
reposentinel config validate
reposentinel config validate --config configs/reposentinel.example.yaml
```

示例 YAML：[configs/reposentinel.example.yaml](https://github.com/Silentely/Repo-Sentinel/blob/main/configs/reposentinel.example.yaml)  
环境变量清单也可对照仓库根目录 [`.env.example`](https://github.com/Silentely/Repo-Sentinel/blob/main/.env.example)。

---

## 环境变量一览

### HTTP

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_HTTP_ADDR` | 监听地址，须含主机与端口，如 `0.0.0.0:8080` |
| `REPOSENTINEL_PUBLIC_BASE_URL` | 对外 URL；生产用 HTTPS（影响 Cookie 等） |

### 数据库

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_DATABASE_DRIVER` | `sqlite`（默认）或 `postgres` |
| `REPOSENTINEL_DATABASE_URL` | SQLite：`file:/path/to.db`；Postgres：标准连接串 |
| `REPOSENTINEL_DATABASE_MAX_OPEN_CONNS` | 最大打开连接（Postgres；SQLite 固定单连接） |
| `REPOSENTINEL_DATABASE_MAX_IDLE_CONNS` | 最大空闲连接（Postgres） |

### 管理员与首次 setup

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_ADMIN_USERNAME` | 首次启动创建唯一管理员；须与密码成对 |
| `REPOSENTINEL_ADMIN_PASSWORD` | 首次密码；**已有管理员时不会覆盖** |
| `REPOSENTINEL_ADMIN_SESSION_TTL` | Session 有效期，如 `24h` |
| `REPOSENTINEL_SETUP_ALLOW_REMOTE` | 是否允许非 loopback 做 Web setup，默认 `false` |

### 主密钥

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_ENCRYPTION_KEY` | 当前主密钥；解码后 **正好 32 字节**（base64 或 hex） |
| `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS` | 轮换期间的上一把密钥 |

```bash
openssl rand -base64 32
openssl rand -hex 32
```

### GitHub

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_GITHUB_APP_ID` | App 数字 ID |
| `REPOSENTINEL_GITHUB_CLIENT_ID` | Client ID |
| `REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH` | 私钥文件路径 |
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | 当前 Webhook Secret |
| `REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET` | 轮换中的上一把 Secret |
| `REPOSENTINEL_EXTERNAL_PAT` | 外部公开仓访问用 PAT（可选） |

### 通知

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_TELEGRAM_TOKEN` | Bot Token |
| `REPOSENTINEL_TELEGRAM_CHAT_ID` | 目标 Chat ID |
| `REPOSENTINEL_HTTP_WEBHOOK_URL` | 通用 HTTPS Webhook |
| `REPOSENTINEL_HTTP_WEBHOOK_SECRET` | Webhook 签名 Secret |
| `REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE` | 是否允许私网目标，默认 `false` |

### 日志

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_LOG_FORMAT` | `json` 或 `text` |
| `REPOSENTINEL_LOG_LEVEL` | `debug` / `info` / `warn` / `error` |
| `LOG_FORMAT` / `LOG_LEVEL` | 标准别名；同时存在时覆盖上面两个 |

### 指标与更新检查

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_METRICS_ENABLED` | 是否暴露 `GET /metrics`，默认 `true` |
| `REPOSENTINEL_METRICS_TOKEN` | 非空则要求 `Authorization: Bearer …` |
| `REPOSENTINEL_UPDATE_CHECK` | 关于页远程版本检查，默认开启 |
| `REPOSENTINEL_UPDATE_CHECK_URL` | 自定义检查源（须 HTTPS） |
| `REPOSENTINEL_UPDATE_CHECK_TOKEN` | API 路径可选 Token |

### 通知聚合（可选）

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_AGGREGATION_WINDOW` | 短时合并窗口，如 `60s` |
| `REPOSENTINEL_AGGREGATION_BURST_THRESHOLD` | 突发条数阈值 |
| `REPOSENTINEL_AGGREGATION_BURST_WINDOW` | 突发统计窗口，如 `5m` |

配置错误返回稳定 `validation_failed`，不会回显密码、连接串或主密钥原文。

---

## YAML

适合放监听地址、库类型、开关等非敏感项：

```bash
reposentinel serve --config configs/reposentinel.example.yaml
```

密码、主密钥、Secret、Token 仍用环境变量覆盖。

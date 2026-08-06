# 配置参考

配置合并顺序：**默认值 → YAML 文件 → 环境变量**（环境变量优先）。  
Secret（密码、主密钥、Webhook Secret、Token）不要写入仓库内 YAML。

GitHub App 相关项（App ID / Client ID / 私钥 / 入站 Webhook Secret / Public Base URL）还可在管理台 **GitHub App** 页填写：未用环境变量设置的字段会加密写入数据库并热更新；**同一字段若已设环境变量，则以环境变量为准**（页面显示锁定）。`REPOSENTINEL_EXTERNAL_PAT` 仍仅环境变量。

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

应用启动会自动执行迁移，因此数据库用户必须能在目标 schema 建表（至少 `CREATE` on `public`）。仅有 `USAGE` 不够。

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
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | **入站**：GitHub → 本服务 Webhook 签名 Secret（`X-Hub-Signature-256`） |
| `REPOSENTINEL_GITHUB_WEBHOOK_PREVIOUS_SECRET` | 轮换中的上一把入站 Secret |
| `REPOSENTINEL_EXTERNAL_PAT` | 外部公开仓访问用 PAT（可选） |

### 通知

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_TELEGRAM_TOKEN` | Bot Token |
| `REPOSENTINEL_TELEGRAM_CHAT_ID` | 目标 Chat ID |
| `REPOSENTINEL_HTTP_WEBHOOK_URL` | **出站**：本服务 → 你的接收端 HTTPS URL |
| `REPOSENTINEL_HTTP_WEBHOOK_SECRET` | **出站**签名 Secret（`X-GitHub-Monitor-Signature-256`）；与 GitHub 入站 Secret 无关 |
| `REPOSENTINEL_HTTP_WEBHOOK_ALLOW_PRIVATE` | 是否允许私网目标，默认 `false` |

> 管理台「渠道配置」页的 HTTP 签名 Secret 与 `REPOSENTINEL_HTTP_WEBHOOK_SECRET` 是同一用途：环境变量仅在渠道尚不存在时种子写入数据库；之后以页面保存为准，无需两边重复填写。`REPOSENTINEL_GITHUB_WEBHOOK_SECRET` 只服务 GitHub 入站，不可混用。

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

### AI 集成（可选）

AI 用于三处：每日摘要 / 周报 / 月报正文生成（`digest_enabled`），以及实时安全告警的影响分析与处理建议（`triage_enabled`）。默认关闭；开启时须提供 API Key，支持任意 OpenAI 兼容端点（可接 Ollama 等本地模型）。

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_AI_ENABLED` | AI 总开关，默认 `false` |
| `REPOSENTINEL_AI_API_KEY` | API Key，`enabled=true` 时必填 |
| `REPOSENTINEL_AI_BASE_URL` | OpenAI 兼容端点，默认 `https://api.openai.com/v1` |
| `REPOSENTINEL_AI_MODEL` | 模型名，默认 `gpt-4o-mini` |
| `REPOSENTINEL_AI_TIMEOUT` | 单次请求超时，默认 `30s` |
| `REPOSENTINEL_AI_MAX_TOKENS` | 输出 token 上限，默认 `800` |
| `REPOSENTINEL_AI_DIGEST_ENABLED` | 是否启用 AI 摘要，默认 `true` |
| `REPOSENTINEL_AI_TRIAGE_ENABLED` | 是否启用安全告警分诊，默认 `true` |

AI 不可用（未配置、超时、服务端错误）时自动降级：摘要回退模板正文、告警保持原文，不影响通知投递。接入本地模型的 YAML 示例见下节。

### Agent 访问（可选）

面向 AI Agent 的只读访问令牌（OAuth 2.0 client_credentials）。未配置时 Agent 发现元数据照常发布，但 `/oauth/token` 拒绝签发令牌。令牌由主密钥派生密钥（HS256）签名，有效期 1 小时；主密钥轮换后旧令牌自动作废。启用方式见示例配置 `configs/reposentinel.example.yaml` 的 `oauth` 段。

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_OAUTH_CLIENT_ID` | Agent 客户端标识，默认 `reposentinel-agent` |
| `REPOSENTINEL_OAUTH_CLIENT_SECRET` | Agent 客户端密钥（必填才可签发令牌） |

除环境变量外，AI 配置也可在管理台「关于与设置 → AI 集成」编辑：环境变量已设置的字段在管理台锁定（写入返回 `ai_field_locked`），未设置的字段由数据库补充；API Key 经主密钥 AES-GCM 加密存库，管理台不回显明文。修改保存后热生效，无需重启。

该区块提供「测试连通性」按钮：以当前生效配置向端点发送一次最小对话并返回耗时与结果，未锁定字段可携带表单中的临时值（便于保存前先验证端点 / 模型 / 密钥），测试不写库、不改变运行时。测试仅发送固定最小消息（不携带仓库数据），请求会发往配置的 Base URL——该地址可指向任意 http(s) 端点（含本地 / 内网模型网关），由管理员自行配置并承担相应风险。

> 隐私提示：启用 AI 后，事件标题、仓库名与告警信息会发送至所配置的模型服务（第三方云端或本地网关）。敏感环境建议接入本地模型（如 Ollama），避免仓库信息外泄。

配置错误返回稳定 `validation_failed`，不会回显密码、连接串或主密钥原文。

---

## YAML

适合放监听地址、库类型、开关等非敏感项：

```bash
reposentinel serve --config configs/reposentinel.example.yaml
```

密码、主密钥、Secret、Token 仍用环境变量覆盖。

接入本地 Ollama 的示例（API Key 可任意占位，本地网关不校验）：

```yaml
ai:
  enabled: true
  base_url: "http://127.0.0.1:11434/v1"
  model: "llama3.1"
  timeout: "60s"
```

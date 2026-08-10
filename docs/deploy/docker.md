# Docker 部署

生产推荐：拉取 GHCR 镜像 + 数据卷（默认 SQLite）+ 反向代理 HTTPS。  
**不要**在部署机本地 `docker build`。

## 镜像

```text
ghcr.io/silentely/repo-sentinel:latest
```

也可钉死版本，例如 `ghcr.io/silentely/repo-sentinel:v0.3.9`。  
若包为私有，先登录：

```bash
echo "$GITHUB_TOKEN" | docker login ghcr.io -u USERNAME --password-stdin
```

维护者发版与标签说明见 [发布与镜像](/reference/release)（用户部署可跳过）。

## 1. 准备环境变量

```bash
cp .env.example .env
```

最少需要：

| 变量 | 说明 |
|------|------|
| `REPOSENTINEL_ENCRYPTION_KEY` | 主密钥，`openssl rand -base64 32` 生成，**必须**与数据一并备份 |
| `REPOSENTINEL_PUBLIC_BASE_URL` | 对外访问地址，生产用 `https://你的域名` |
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | GitHub App Webhook Secret |

建议同时配置管理员（否则首次用本机浏览器 Web setup）：

```bash
REPOSENTINEL_ADMIN_USERNAME=admin
REPOSENTINEL_ADMIN_PASSWORD=请换成强密码
```

经域名 / 反向代理做首次 setup 时，需临时 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`，完成后关掉。  
完整变量表见 [配置参考](/reference/configuration)。

## 2. 数据库：SQLite（默认）或 PostgreSQL

Compose 默认已是 **SQLite**，数据在卷 `reposentinel-data` 的 `/data/reposentinel.db`：

```yaml
REPOSENTINEL_DATABASE_DRIVER: "sqlite"
REPOSENTINEL_DATABASE_URL: "file:/data/reposentinel.db"
```

改用 **PostgreSQL** 时，在 `.env` 或 compose `environment` 中设置：

```bash
REPOSENTINEL_DATABASE_DRIVER=postgres
REPOSENTINEL_DATABASE_URL=postgres://用户:密码@db主机:5432/reposentinel?sslmode=require
```

说明：

- `driver` 只能是 `sqlite` 或 `postgres`（不要写 `postgresql`）
- Postgres **必须**提供非空 `REPOSENTINEL_DATABASE_URL`
- **密码中的 `#` `@` `(` 等必须 URL 编码**，否则启动即 `database_unavailable`（见 [配置参考](/reference/configuration)）
- 库需事先建好；应用启动时自动跑迁移
- 仓库里的 `deployments/test/postgres.compose.yml` 仅供开发测试，**不是**生产模板

可选连接池（仅 Postgres 有意义）：

```bash
REPOSENTINEL_DATABASE_MAX_OPEN_CONNS=10
REPOSENTINEL_DATABASE_MAX_IDLE_CONNS=5
```

## 3. 启动

### Docker Compose（推荐）

根目录 `docker-compose.yml` 默认镜像为 `ghcr.io/silentely/repo-sentinel:latest`。

```bash
docker compose pull
docker compose up -d
curl -fsS http://127.0.0.1:8080/health/ready
```

### docker run

```bash
docker run -d \
  --name reposentinel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v reposentinel-data:/data \
  -e REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
  -e REPOSENTINEL_PUBLIC_BASE_URL=https://monitor.example.com \
  -e REPOSENTINEL_DATABASE_DRIVER=sqlite \
  -e REPOSENTINEL_DATABASE_URL=file:/data/reposentinel.db \
  -e REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
  -e REPOSENTINEL_GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  ghcr.io/silentely/repo-sentinel:latest
```

### 自检

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
docker compose exec reposentinel /reposentinel version
```

浏览器打开应用地址，完成管理员创建（若未用环境变量预置）。公网请接 [反向代理](/deploy/reverse-proxy)。

## 4. GitHub App（创建表单逐项）

RepoSentinel **不走用户 OAuth 登录 GitHub**；认证是「App JWT → Installation Token」。因此创建页里与「Identifying and authorizing users」相关的 Callback / Device Flow 均可空着。

打开 [Create GitHub App](https://github.com/settings/apps/new)（组织可用 `https://github.com/organizations/<org>/settings/apps/new`）。

### 4.1 基本信息

| 字段 | 怎么填 |
|------|--------|
| **GitHub App name** | 任意全局唯一名，例如 `RepoSentinel` 或 `RepoSentinel-<你的ID>` |
| **Description** | 可选，如「自托管仓库值守」 |
| **Homepage URL** | 你的管理台公网地址，如 `https://monitor.example.com`；本地可先填 `http://127.0.0.1:8080`（Webhook 仍需 GitHub 能访问的 HTTPS） |

### 4.2 Identifying and authorizing users（可全部跳过）

| 字段 | 怎么填 |
|------|--------|
| **Callback URL** | **留空**（产品无 OAuth 回调路由） |
| **Expire user authorization tokens** | 不勾 |
| **Request user authorization (OAuth) during installation** | **不勾** |
| **Enable Device Flow** | **不勾** |

### 4.3 Post installation

| 字段 | 怎么填 |
|------|--------|
| **Setup URL** | 可选；可填管理台地址，安装后跳回你的控制台 |
| **Redirect on update** | 可选；一般不勾 |

### 4.4 Webhook（必填）

| 字段 | 怎么填 |
|------|--------|
| **Active** | **必须勾选** |
| **Webhook URL** | `https://你的域名/webhooks/github`（与 `REPOSENTINEL_PUBLIC_BASE_URL` + 路径一致；管理台「GitHub App」页可复制） |
| **Secret** | 随机串，例如 `openssl rand -hex 32`；**同一值**写入环境变量 `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` |

> 本地开发可用 [smee.io](https://smee.io) / ngrok 等把公网 HTTPS 转到本机；Secret 仍要与环境变量一致。

### 4.5 Repository permissions（建议只读）

权限选好后，下方「Subscribe to events」才会出现对应事件。

| 权限 | Access | 用途 |
|------|--------|------|
| **Metadata** | Read-only | 必选（仓库基础信息） |
| **Contents** | Read-only | 对账/元数据 |
| **Issues** | Read-only | Issue Webhook + API 对账 |
| **Pull requests** | Read-only | PR Webhook + 对账 |
| **Actions** | Read-only | `workflow_run` 与对账 |
| **Dependabot alerts** | Read-only | Dependabot 告警 |
| **Code scanning alerts** | Read-only | Code Scanning |
| **Secret scanning alerts** | Read-only | Secret Scanning |

- **Organization permissions**：全部 **No access**（除非你明确需要组织级能力）。  
- **Account permissions**：全部 **No access**。  
- 不要选 Write，除非你清楚自己在做什么（本产品不写回 GitHub）。

### 4.6 Subscribe to events（勾选）

| 事件 | 是否需要 |
|------|----------|
| **Issues** | 要 |
| **Pull request** | 要 |
| **Workflow run** | 要 |
| **Dependabot alert** | 要（安全） |
| **Code scanning alert** | 要（安全） |
| **Secret scanning alert** | 要（安全） |
| **Installation** | 要 |
| **Installation repositories** | 要 |
| **Repository** | 要 |
| Installation target / Meta / Security advisory | 可不勾（处理器会忽略未处理类型） |

创建页若暂时看不到某事件，先提高对应 Repository permission，保存后再回来勾事件。

### 4.7 Where can this GitHub App be installed?

| 选项 | 建议 |
|------|------|
| **Only on this account** | 个人自用推荐 |
| **Any account** | 需要装到多个组织/账号时再选 |

### 4.8 创建后必做

1. **Generate a private key**，下载 `.pem`  
2. 记下 **App ID**、**Client ID**（对账核心依赖 **App ID + 私钥**）  
3. **写入运行时（二选一）**  

**方式 A · 管理台（推荐上手）**  
登录后打开 **GitHub App** 页，填写 App ID / Client ID / Public Base URL / Webhook Secret，并粘贴 PEM 或填服务器路径 → **保存**（加密入库，立即生效，无需重启）。

**方式 B · 环境变量（适合 Docker / K8s Secret）**

```bash
REPOSENTINEL_GITHUB_APP_ID=123456
REPOSENTINEL_GITHUB_CLIENT_ID=Iv1.xxxxxxxx
REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH=/secrets/github-app.pem
REPOSENTINEL_GITHUB_WEBHOOK_SECRET=与_App_Webhook_Secret_相同
REPOSENTINEL_PUBLIC_BASE_URL=https://你的域名
```

> **优先级**：环境变量 **高于** 管理台数据库配置。某字段已用环境变量设置时，管理台该字段会显示「锁定」，不能覆盖。  
> 私钥路径示例：`REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH=/secrets/github-app.pem`，compose 中挂载 pem；或改用管理台粘贴 PEM。

4. **Install App**（在 GitHub 完成，管理台不能代替授权）  
   - 打开 [已安装的 Apps](https://github.com/settings/installations) 或 App 设置页的 Install  
   - 选择 All / 指定仓库后保存；GitHub 会向 `/webhooks/github` 推送 `installation`  
5. **确认入库**  
   - 管理台 **GitHub App** → Installation 列表出现账号  
   - **仪表盘** 出现仓库（状态「基线中」）  
   - 若日志已 `accepted` 但仪表盘仍空：点 **「从 GitHub 同步仓库」**（`POST /api/v1/github/sync-repositories`）补拉  
6. 对需要监控的仓点 **「完成基线」** 后再发实时通知

## 5. Telegram / 其他通知

```bash
REPOSENTINEL_TELEGRAM_TOKEN=...
REPOSENTINEL_TELEGRAM_CHAT_ID=...
# 或
# REPOSENTINEL_HTTP_WEBHOOK_URL=https://hooks.example.com/...
# REPOSENTINEL_HTTP_WEBHOOK_SECRET=...
```

也可登录后在「渠道配置」页保存。Token 用主密钥加密入库。

## 6. 备份

```bash
# 维护窗口备份数据卷，并同时保管 REPOSENTINEL_ENCRYPTION_KEY
docker compose exec reposentinel /reposentinel backup --output /tmp/backup
```

详见 [运维手册](/reference/ops)。

## 本地自建镜像（可选）

仅开发或排障：

```bash
docker build -t reposentinel:local \
  --build-arg VERSION="$(tr -d '[:space:]' < VERSION)" \
  --build-arg BUILD_CHANNEL=local .
```

生产请继续用 `ghcr.io/silentely/repo-sentinel:latest`（或钉死的 `v*`）。

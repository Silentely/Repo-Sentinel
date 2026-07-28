# Docker 部署

生产推荐：拉取 GHCR 镜像 + 数据卷（默认 SQLite）+ 反向代理 HTTPS。  
**不要**在部署机本地 `docker build`。

## 镜像

```text
ghcr.io/silentely/repo-sentinel:latest
```

也可钉死版本，例如 `ghcr.io/silentely/repo-sentinel:v0.3.4`。  
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

- `driver` 只能是 `sqlite` 或 `postgres`
- Postgres **必须**提供非空 `REPOSENTINEL_DATABASE_URL`
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

## 4. GitHub App Webhook

1. 创建 GitHub App，按需勾选 Issues / Pull requests / Actions / 安全告警等只读权限  
2. Webhook URL：`https://你的域名/webhooks/github`  
3. Secret 写入 `REPOSENTINEL_GITHUB_WEBHOOK_SECRET`  
4. 订阅：`issues`、`pull_request`、`workflow_run`、三类 `*_alert`、`installation`、`installation_repositories`、`repository`  
5. 安装到仓库后，新仓先为**基线中**；管理后台点「完成基线」后才发实时通知  

私钥路径示例：`REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH=/secrets/github-app.pem`，并在 compose 中挂载 pem 文件。

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

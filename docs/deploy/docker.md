# Docker 部署

生产推荐：拉取 GHCR 镜像 + SQLite 数据卷 + 反向代理 HTTPS。默认 **不要** 在部署机本地 `docker build`。

## 镜像策略

| 触发条件 | 镜像标签 | 架构 | 用途 |
|----------|----------|------|------|
| 推送到 `main` | `main`、`main-<sha>` | amd64 | 主干滚动 |
| 推送到 `dev` | `dev`、`dev-<sha>` | amd64 | 开发 / 预发 |
| Git 标签 `v*` | `vX.Y.Z`、`latest` | amd64 + arm64 | 正式发版 |
| 手动 `workflow_dispatch` | 按当前分支 / tag 规则 | 同上 | 应急补镜像 |

> 发版：合并到 `main`（更新 `main` / `main-<sha>`）→ 打 `vX.Y.Z` 并 push tag（更新 `vX.Y.Z` / `latest`）。  
> 正式 tag 因双架构 + QEMU，通常比 main 的单架构构建更久（约 15–30 分钟）；main/dev 约 3–8 分钟属正常。详见 [发布与镜像](/reference/release)。

镜像仓库：

```text
ghcr.io/silentely/repo-sentinel
```

## 前置

```bash
cp .env.example .env
# 必填：
# REPOSENTINEL_ENCRYPTION_KEY=$(openssl rand -base64 32)
# REPOSENTINEL_GITHUB_WEBHOOK_SECRET=...
# 建议：管理员用户名/密码，或首次本机 Web setup
```

若 GHCR 包为私有，先登录：

```bash
echo "$GITHUB_TOKEN" | docker login ghcr.io -u USERNAME --password-stdin
```

## 快速部署

### docker run

```bash
docker run -d \
  --name reposentinel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v reposentinel-data:/data \
  -e REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
  -e REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
  -e REPOSENTINEL_GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  ghcr.io/silentely/repo-sentinel:latest
```

### Docker Compose（推荐）

仓库根目录 `docker-compose.yml` 默认：

```yaml
image: ghcr.io/silentely/repo-sentinel:latest
```

```bash
cp .env.example .env
docker compose pull
docker compose up -d
curl -fsS http://127.0.0.1:8080/health/ready
```

数据在卷 `reposentinel-data` 的 `/data/reposentinel.db`。

固定版本或跟主干：

```bash
# image: ghcr.io/silentely/repo-sentinel:v0.3.4
# image: ghcr.io/silentely/repo-sentinel:main
```

### 上线后自检

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
docker compose exec reposentinel /reposentinel version
```

## GitHub App Webhook

1. 创建 GitHub App，只读权限覆盖 Issues、Pull requests、Actions、Dependabot / Code scanning / Secret scanning alerts（按需勾选）
2. Webhook URL：`https://你的域名/webhooks/github`
3. Secret 写入 `REPOSENTINEL_GITHUB_WEBHOOK_SECRET`
4. 订阅：`issues`、`pull_request`、`workflow_run`、三类 `*_alert`、`installation`、`installation_repositories`、`repository`
5. 安装到仓库后，首次仓库为 **基线中**；管理后台点「完成基线」后才发实时通知

## Telegram

在 `.env` 设置 `REPOSENTINEL_TELEGRAM_TOKEN` 与 `REPOSENTINEL_TELEGRAM_CHAT_ID`，或登录后在「渠道配置」页保存。Token 使用主密钥 AES-GCM 加密入库。

## 备份

```bash
docker compose exec reposentinel /reposentinel version
# 维护窗口备份数据卷，并同时保管 REPOSENTINEL_ENCRYPTION_KEY
# 也可用：docker compose exec reposentinel /reposentinel backup --output /tmp/backup.db
```

更完整说明见 [运维手册](/reference/ops)。

## 本地源码镜像（可选）

仅开发或排障需要自建时：

```bash
docker build -t reposentinel:local \
  --build-arg VERSION="$(tr -d '[:space:]' < VERSION)" \
  --build-arg BUILD_CHANNEL=local .
```

生产路径请继续使用 `ghcr.io/silentely/repo-sentinel:latest`（或钉死的 `v*` / 跟进的 `main`）。

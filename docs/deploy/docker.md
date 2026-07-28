# Docker 部署

生产最小闭环：单容器 + SQLite 卷 + 反向代理 HTTPS。

## 前置

```bash
cp .env.example .env
# 必填：
# REPOSENTINEL_ENCRYPTION_KEY=$(openssl rand -base64 32)
# REPOSENTINEL_GITHUB_WEBHOOK_SECRET=...
# 建议：管理员用户名/密码 或首次本机 setup
```

## 启动

```bash
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/health/ready
```

数据持久化在卷 `reposentinel-data` 的 `/data/reposentinel.db`。

## GitHub App Webhook

1. 创建 GitHub App，权限见设计规格（Issues/PR/Actions/安全告警只读）
2. Webhook URL：`https://你的域名/webhooks/github`
3. Secret 写入 `REPOSENTINEL_GITHUB_WEBHOOK_SECRET`
4. 订阅：`issues`、`pull_request`、`workflow_run`、三类 `*_alert`、`installation`、`installation_repositories`、`repository`
5. 安装到仓库后，首次仓库为 **基线中**；管理后台点「完成基线」后才发实时通知

## Telegram

在 `.env` 设置 `REPOSENTINEL_TELEGRAM_TOKEN` 与 `REPOSENTINEL_TELEGRAM_CHAT_ID`，或登录后在「渠道配置」页保存。Token 使用主密钥 AES-GCM 加密入库。

## 备份

```bash
# 停止或使用 sqlite3 .backup
docker compose exec reposentinel /reposentinel version
# 备份卷内数据库文件（维护窗口）并同时备份 REPOSENTINEL_ENCRYPTION_KEY
```

更完整说明见 [运维手册](/reference/ops)。

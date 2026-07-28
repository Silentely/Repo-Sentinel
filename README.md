# RepoSentinel

自托管的 GitHub 仓库动态与安全告警监控平台。

> **当前版本 v0.2.0 = 生产 MVP 闭环**  
> Webhook 验签入库 → 规范化（基线抑制 / 乱序保护）→ 规则引擎 → Outbox → Telegram / HTTP Webhook  
> 管理仪表盘、渠道配置、Docker Compose 单容器部署。

## 5 分钟上线（Docker）

```bash
cp .env.example .env
# 编辑 .env：至少设置 REPOSENTINEL_ENCRYPTION_KEY 与 Webhook Secret
docker compose up -d --build
curl -fsS http://127.0.0.1:8080/health/ready
```

打开 <http://127.0.0.1:8080> 完成管理员初始化（或用环境变量预置）。

GitHub App Webhook URL：`https://你的域名/webhooks/github`  
详细步骤：[Docker 部署](docs/deploy/docker.md)

## 文档

| 文档 | 说明 |
|------|------|
| [快速开始](docs/guide/quick-start.md) | 源码构建与启动 |
| [Docker 部署](docs/deploy/docker.md) | 生产 Compose |
| [配置参考](docs/reference/configuration.md) | 环境变量与主密钥 |
| [实现状态](docs/reference/implementation-status.md) | 规格对照 |
| [文档总览](docs/README.md) | 完整导航 |

```bash
npm install && npm run docs:dev   # http://127.0.0.1:5174
```

## 源码开发

```bash
pnpm --dir web install && pnpm --dir web build
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production

REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
REPOSENTINEL_GITHUB_WEBHOOK_SECRET=dev-secret \
.tmp/reposentinel serve
```

验证：

```bash
go test ./...
pnpm --dir web test -- --run
pnpm --dir web typecheck
```

## 能力边界（诚实清单）

**已具备**

- 唯一管理员、Session/CSRF、健康检查
- `/webhooks/github` 双 Secret 验签、delivery 幂等
- Issue/PR/workflow_run/安全告警/installation/repository 规范化
- 基线抑制、陈旧写入丢弃、事件指纹
- Outbox + Telegram/HTTP 投递与死信重试
- 仪表盘 KPI、仓库列表、完成基线、通知渠道配置
- Dockerfile + docker-compose（SQLite）

**仍简化 / 后续**

- 自有仓 GitHub API 自动对账与 Installation Token 缓存刷新
- 外部仓 Issues API 轮询（目前可登记，依赖 Webhook 不适用于纯外部仓）
- 通知时间窗聚合、每日摘要调度
- GHCR 多架构发布流水线、应用内 backup CLI

设计规格：`docs/superpowers/specs/2026-07-26-reposentinel-design.md`  
实施计划：`docs/superpowers/plans/2026-07-28-production-mvp.md`

# RepoSentinel

自托管的 GitHub 仓库动态与安全告警监控平台。

> **当前版本 v0.1.0 = Phase 1 基础平台**  
> 已交付：配置与主密钥、SQLite/PostgreSQL 迁移、唯一管理员、Session/CSRF、健康检查、CLI、React 认证壳。  
> **未交付**：GitHub Webhook 采集、对账、规则引擎、通知、完整仪表盘、正式 Docker 镜像。  
> 详见 [实现状态](docs/reference/implementation-status.md) 与 [功能与路线图](docs/features.md)。

## 文档

结构化文档站（对齐 [TG-SignPulse](https://github.com/Silentely/TG-SignPulse) 的 VitePress 布局）：

| 文档 | 说明 |
|------|------|
| [快速开始](docs/guide/quick-start.md) | 构建、启动、首次管理员 |
| [配置参考](docs/reference/configuration.md) | 环境变量与主密钥 |
| [管理员](docs/guide/administrator.md) | Setup、Session、CLI 重置密码 |
| [实现状态](docs/reference/implementation-status.md) | 规格对照与验证证据 |
| [文档总览](docs/README.md) | 完整导航 |

本地预览文档站：

```bash
npm install
npm run docs:dev
```

## 先运行起来（Phase 1）

需要 Go 1.26+、Node.js 24+、pnpm 10.34.5。

```bash
mkdir -p .tmp
pnpm --dir web install
pnpm --dir web build
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production

REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel.db \
.tmp/reposentinel serve
```

打开 <http://127.0.0.1:8080>，完成唯一管理员初始化后登录。

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
curl -fsS http://127.0.0.1:8080/api/v1/setup/status
```

## 开发命令

```bash
# API
REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel-dev.db \
go run ./cmd/reposentinel serve

# 前端（另开终端；代理 /api 与 /health）
pnpm --dir web dev

# 验证
go test ./...
go vet ./...
pnpm --dir web test -- --run
pnpm --dir web typecheck
pnpm --dir web build
npm run docs:build
```

CLI 子命令：`serve` · `version` · `config validate` · `admin reset-password`。

## 安全边界（Phase 1）

- Session：HttpOnly Cookie；CSRF：双提交
- 登录进程内 IP 限流
- Setup 默认仅 loopback；公网须 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`
- `/api` 与 `/health` 未知路径返回 JSON，不被 SPA 吞掉
- 生产静态资源长缓存；`index.html` no-cache

## 路线图

设计规格：`docs/superpowers/specs/2026-07-26-reposentinel-design.md`（内部设计稿，默认不编入文档站）。

1. **Phase 1（当前）** — 运行时与认证  
2. GitHub App + Webhook Inbox + 事件规范化  
3. 自有仓基线与对账、Workflow 状态机  
4. 外部公开仓轮询  
5. Rule Engine、Outbox、Telegram / HTTP  
6. 完整管理后台  
7. Docker / GHCR / 发布  
8. 备份 CLI、指标、加固与完整 E2E  

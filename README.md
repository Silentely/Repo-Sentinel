<p align="center">
  <img src="docs/public/logo.svg" width="80" height="80" alt="RepoSentinel Logo">
</p>

<h1 align="center">RepoSentinel</h1>

<p align="center">
  <strong>自托管 GitHub 仓库值守平台</strong><br>
  Webhook · 对账补漏 · 安全告警 · Telegram 通知
</p>

<p align="center">
  <img src="https://img.shields.io/badge/go-1.26+-00ADD8" alt="Go">
  <img src="https://img.shields.io/badge/node-24+-green" alt="Node.js">
  <img src="https://img.shields.io/badge/version-0.4.0-C45C26" alt="Version">
  <img src="https://img.shields.io/badge/license-see%20repo-lightgrey" alt="License">
</p>

<p align="center">
  <a href="https://repo.cosr.eu.org/guide/quick-start.html"><strong>快速开始</strong></a>
  ·
  <a href="https://repo.cosr.eu.org/deploy/docker.html">Docker 部署</a>
  ·
  <a href="https://repo.cosr.eu.org/README.html">文档总览</a>
  ·
  <a href="https://repo.cosr.eu.org/reference/configuration.html">配置参考</a>
</p>

---

## 项目简介

RepoSentinel 是面向个人与小团队的 **GitHub 仓库监控控制台**。它通过 GitHub App Webhook 实时接收 Issue、Pull Request、Actions 与安全告警，用 API 对账补漏，并把重要变化推送到 Telegram 或通用 HTTP Webhook。默认使用 SQLite，可选 PostgreSQL；单进程部署，管理后台嵌入同一二进制。

> 适合公网 VPS / 自建机房：一个容器（或一个二进制）即可完成值守，不依赖多用户 SaaS。

---

## 功能概览

| 模块 | 能力 |
|------|------|
| **实时采集** | GitHub App Webhook：Issue / PR / star / watch / workflow_run / Dependabot / Code Scanning / Secret Scanning |
| **安装与仓库** | installation / installation_repositories / repository 生命周期；自有仓与外部公开仓登记 |
| **可靠入库** | Delivery 幂等、事件指纹、乱序陈旧写入丢弃、首次基线抑制通知洪流 |
| **通知** | Outbox 持久化投递；Telegram 富文本；HTTPS Webhook（HMAC 签名）；失败重试与死信重试 |
| **定期报告（可选）** | 每日摘要 + 每周/每月报告；正文可由 LLM 生成自然语言总结，失败自动回退模板 |
| **AI 告警分诊（可选）** | 新安全告警通知附带 AI 影响分析与处理建议；默认关闭，支持 OpenAI 兼容端点（可接本地模型） |
| **管理后台** | 仪表盘 KPI（含 Star 增长曲线，7/30/90/全部）、仓库与基线、最近事件、投递记录、渠道配置、亮/暗主题 |
| **安全基线** | 单管理员、Argon2id、Session + CSRF、主密钥 AES-GCM、敏感配置掩码 |
| **运维** | 健康检查、`/metrics`、结构化日志、Docker/GHCR、Compose |

---

## 技术栈

```
┌─────────────────────────────────────────────────────────┐
│  Frontend          React 19 + TypeScript                 │
│                    TanStack Router / Query               │
│                    Vite + Tailwind CSS                   │
├─────────────────────────────────────────────────────────┤
│  Backend           Go 模块化单体 + chi                    │
│                    Ent + Atlas 迁移                      │
│                    slog 结构化日志                       │
├─────────────────────────────────────────────────────────┤
│  Storage           默认 SQLite（WAL）                     │
│                    可选 PostgreSQL                       │
├─────────────────────────────────────────────────────────┤
│  Integration       GitHub App Webhook + REST             │
│                    Telegram Bot API / HTTP Webhook       │
├─────────────────────────────────────────────────────────┤
│  Infrastructure    GHCR 镜像 · Compose 单容器样例        │
└─────────────────────────────────────────────────────────┘
```

---

## 快速开始

### 前置条件

- Docker 24+ 与 Docker Compose（推荐生产路径）
- 或：Go 1.26+、Node.js 24+、pnpm 10.34.5（源码构建）
- 一个 GitHub App（接收 Webhook）与可选的 Telegram Bot

### Docker Compose（推荐：拉取 GHCR，无需本地构建）

```bash
cp .env.example .env
# 必填 REPOSENTINEL_ENCRYPTION_KEY（openssl rand -base64 32）
# 建议填写 Webhook Secret、管理员账号或首次本机初始化

docker compose pull
docker compose up -d
curl -fsS http://127.0.0.1:8080/health/ready
```

默认镜像：`ghcr.io/silentely/repo-sentinel:latest`。私有包需先 `docker login ghcr.io`。

常用环境变量（完整见 [`.env.example`](.env.example) 与 [配置参考](https://repo.cosr.eu.org/reference/configuration.html)）：

| 变量 | 用途 |
|------|------|
| `REPOSENTINEL_ENCRYPTION_KEY` | 主密钥（必填，`openssl rand -base64 32`） |
| `REPOSENTINEL_PUBLIC_BASE_URL` | 对外 URL，生产用 HTTPS |
| `REPOSENTINEL_DATABASE_DRIVER` | `sqlite`（默认）或 `postgres` |
| `REPOSENTINEL_DATABASE_URL` | SQLite 文件 DSN 或 Postgres 连接串 |
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | GitHub 入站 Webhook Secret（也可在管理台 GitHub App 页填写） |
| `REPOSENTINEL_ADMIN_USERNAME` / `PASSWORD` | 可选，预置唯一管理员 |

浏览器打开 <http://127.0.0.1:8080>，完成唯一管理员创建（或使用环境变量预置）。

GitHub App Webhook URL：

```text
https://你的域名/webhooks/github
```

完整步骤见 [Docker 部署](https://repo.cosr.eu.org/deploy/docker.html) 与 [快速开始](https://repo.cosr.eu.org/guide/quick-start.html)。

### 源码运行

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

---

## 文档

完整使用文档见已部署的**文档站**：[https://repo.cosr.eu.org](https://repo.cosr.eu.org)。

| 文档 | 说明 |
|------|------|
| [快速开始](https://repo.cosr.eu.org/guide/quick-start.html) | 构建、启动、首次管理员 |
| [Docker 部署](https://repo.cosr.eu.org/deploy/docker.html) | 镜像、环境变量、SQLite/Postgres、Webhook |
| [源码部署](https://repo.cosr.eu.org/deploy/source.html) | 本地源码构建与运行 |
| [反向代理](https://repo.cosr.eu.org/deploy/reverse-proxy.html) | HTTPS/反代部署 |
| [配置参考](https://repo.cosr.eu.org/reference/configuration.html) | 全部环境变量、数据库、主密钥 |
| [运维手册](https://repo.cosr.eu.org/reference/ops.html) | 健康检查、备份、升级 |
| [管理员与 Session](https://repo.cosr.eu.org/guide/administrator.html) | 初始化、改密、CLI 重置 |
| [健康与版本](https://repo.cosr.eu.org/guide/health-and-version.html) | 运行状态查询与版本 |
| [列表 API](https://repo.cosr.eu.org/guide/list-api.html) | 事件等列表接口 |
| [系统架构](https://repo.cosr.eu.org/reference/architecture.html) | 模块与数据流 |
| [功能介绍](https://repo.cosr.eu.org/features.html) | 能力边界与非目标 |
| [常见问题](https://repo.cosr.eu.org/faq.html) | 部署与使用排障 |
| [文档总览](https://repo.cosr.eu.org/README.html) | 完整导航 |

以下为面向维护者与贡献者的仓库文档：

| 文档 | 说明 |
|------|------|
| [贡献指南](CONTRIBUTING.md) | Issue / PR 要求与开发约定 |
| [安全策略](SECURITY.md) | 漏洞报告与部署基线 |

---

## 开发与验证

```bash
go test ./...
go vet ./...
pnpm --dir web test -- --run
pnpm --dir web typecheck
pnpm --dir web build
npm run docs:build
```

CLI：`serve` · `version` · `config validate` · `admin reset-password` · `doctor` · `healthcheck` · `backup` · `restore`。

---

## 目录结构（节选）

```text
Repo-Sentinel/
├── cmd/reposentinel/     # 入口
├── internal/             # 配置、认证、HTTP、存储、规范化、规则、通知
├── migrations/           # SQLite / PostgreSQL Atlas 迁移
├── web/                  # React 管理后台（可嵌入二进制）
├── docs/                 # VitePress 文档
├── deployments/          # 测试用 Compose 等
├── Dockerfile
├── docker-compose.yml
└── VERSION
```

---

## 安全提示

- 主密钥 `REPOSENTINEL_ENCRYPTION_KEY` 与数据库**一并**纳入运维备份；密钥丢失则库内加密凭据无法解密。
- 首次 Web setup 默认仅 loopback；公网初始化需显式 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`，完成后关闭。
- Session 使用 HttpOnly Cookie；写操作需要 CSRF。
- Secret Scanning 相关路径不保存真实 Secret 明文。

---

## 版本

当前版本以仓库根目录 [`VERSION`](VERSION) 为准（SemVer，无 `v` 前缀）。构建可通过 `make` 的 ldflags 注入 Git SHA 与构建时间。

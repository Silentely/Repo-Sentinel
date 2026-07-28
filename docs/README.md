# RepoSentinel 文档

> v0.2.0 · 自托管 GitHub 仓库值守：Webhook、安全告警与可靠通知。

## 产品简介

RepoSentinel 用于集中监控 GitHub 仓库的 Issue、Pull Request、Actions 与安全告警，并在重要变化时推送 Telegram 或通用 HTTP Webhook。技术栈为 **Go 模块化单体 + 嵌入式 React 管理后台**，默认 **SQLite**，可选 **PostgreSQL**。

## 适用场景

- 个人 / 小团队多仓库值守
- Actions 失败与恢复感知
- Dependabot / Code Scanning / Secret Scanning 告警闭环
- 关注少量上游开源仓库的 Issue / PR 动态
- Docker / VPS 持续部署

## 技术架构

| 层级 | 技术 |
|------|------|
| 前端 | React 19、TypeScript、TanStack Router/Query、Vite、Tailwind |
| 后端 | Go、chi、Ent、Atlas、slog |
| 认证 | 单管理员、Argon2id、服务端 Session、CSRF |
| 采集 | GitHub App Webhook（主通道）+ 对账扩展点 |
| 通知 | Outbox、Telegram、HTTP Webhook |
| 数据库 | 默认 SQLite（WAL）；可选 PostgreSQL |
| 部署 | Docker 多阶段镜像、Compose 单容器样例 |

## 核心能力

- **实时 Webhook**：Issue / PR / Actions / 三类安全告警 / 安装与仓库事件
- **可靠落库**：Delivery 幂等、事件指纹、乱序保护、首次基线抑制洪流
- **可靠通知**：Outbox 重试、死信重试、渠道密钥加密存储
- **管理后台**：仪表盘、仓库与基线、事件与投递、渠道配置
- **运维友好**：健康检查、配置校验、CLI 密码重置、文档站

## 文档导航

### 开始

| 文档 | 说明 |
|------|------|
| [功能介绍](features.md) | 能力边界与非目标 |
| [快速开始](guide/quick-start.md) | 构建、启动、首次管理员 |

### 使用指南

| 文档 | 说明 |
|------|------|
| [管理员与 Session](guide/administrator.md) | 初始化、登录、改密、CLI 重置 |
| [健康检查与版本](guide/health-and-version.md) | live/ready、版本 API |

### 部署

| 文档 | 说明 |
|------|------|
| [Docker 部署](deploy/docker.md) | Compose、卷、Webhook、Telegram |
| [从源码运行](deploy/source.md) | 开发与嵌入构建 |
| [反向代理](deploy/reverse-proxy.md) | Caddy / Nginx HTTPS |

### 参考

| 文档 | 说明 |
|------|------|
| [配置参考](reference/configuration.md) | 环境变量、YAML、主密钥 |
| [运维手册](reference/ops.md) | 备份、日志、升级 |
| [系统架构](reference/architecture.md) | 模块边界与数据流 |
| [开发规范](reference/development.md) | 测试与迁移 |
| [能力与状态](reference/implementation-status.md) | 已交付能力对照 |

### 帮助

| 文档 | 说明 |
|------|------|
| [常见问题](faq.md) | 启动、setup、密钥、通知 |

## 本地预览文档站

```bash
npm install
npm run docs:dev
```

默认 <http://127.0.0.1:5174>。构建：

```bash
npm run docs:build
```

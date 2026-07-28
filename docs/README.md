# RepoSentinel 文档

> v0.1.0 · 自托管 GitHub 仓库值守平台文档站（结构对齐 [TG-SignPulse](https://github.com/Silentely/TG-SignPulse) 文档站）。

## 产品简介

RepoSentinel 用于集中监控 GitHub 仓库的 Issue、Pull Request、Actions 与安全告警，并在重要变化时推送 Telegram 或通用 HTTP Webhook。技术栈为 **Go 模块化单体 + 嵌入式 React 管理后台**，默认 **SQLite**，可选 **PostgreSQL**。

**当前版本只交付 Phase 1**：可启动的服务、管理员认证、健康检查与文档。Webhook 采集与通知尚未实现，请勿按完整产品能力对外承诺。

## 适用场景（完整产品目标）

- 个人 / 小团队多仓库值守
- Actions 失败与恢复感知
- Dependabot / Code Scanning / Secret Scanning 告警闭环
- 关注少量上游开源仓库的 Issue/PR 动态

## 技术架构（目标）

| 层级 | 技术 |
|------|------|
| 前端 | React 19、TanStack Router/Query、Vite、Tailwind、shadcn 风格组件 |
| 后端 | Go、chi、Ent、Atlas、slog |
| 认证 | 单管理员、Argon2id、服务端 Session、CSRF |
| 采集 | GitHub App Webhook + REST 对账 + 外部仓轮询（后两阶段） |
| 数据库 | 默认 SQLite（WAL）；可选 PostgreSQL |
| 部署 | 单应用容器（规划中）；当前从源码 / 本地二进制运行 |

## 文档导航

### 开始

| 文档 | 说明 |
|------|------|
| [功能与路线图](features.md) | 已交付 vs 规划能力 |
| [快速开始](guide/quick-start.md) | 构建、启动、首次管理员 |

### 使用指南

| 文档 | 说明 |
|------|------|
| [管理员与 Session](guide/administrator.md) | 初始化、登录、改密、CLI 重置 |
| [健康检查与版本](guide/health-and-version.md) | live/ready、版本 API |

### 部署

| 文档 | 说明 |
|------|------|
| [从源码运行](deploy/source.md) | 本机与生产式嵌入构建 |
| [反向代理](deploy/reverse-proxy.md) | Caddy / Nginx HTTPS 要点 |

### 参考

| 文档 | 说明 |
|------|------|
| [配置参考](reference/configuration.md) | 环境变量、YAML、主密钥 |
| [运维手册](reference/ops.md) | 备份恢复、日志、升级注意 |
| [系统架构](reference/architecture.md) | 模块边界与数据流 |
| [开发规范](reference/development.md) | 测试、迁移、前端 |
| [实现状态](reference/implementation-status.md) | 落地检查与验证证据 |

### 帮助

| 文档 | 说明 |
|------|------|
| [常见问题](faq.md) | 启动、setup、密钥、Phase 边界 |

## 本地预览文档站

```bash
npm install
npm run docs:dev
```

默认 <http://127.0.0.1:5174>。构建：

```bash
npm run docs:build
```

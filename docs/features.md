# 功能与路线图

> 当前发布版本 **v0.1.0** 对应设计规格中的 **Phase 1（基础运行时与认证）**。下文区分「已交付」与「规格已定、待实现」，避免把路线图当成已上线能力。

## 产品定位

RepoSentinel 是面向**单用户私有部署**的 GitHub 仓库监控台：

- 集中查看多个自有仓库的 Issue、PR、Actions、安全告警
- 用 GitHub App Webhook 做主通道，用 API 对账补漏
- 关注少量无法安装 App 的外部公开仓库
- 重要事件实时通知，低优先级进每日摘要

## 已交付（Phase 1）

| 能力 | 说明 |
|------|------|
| 配置加载 | 默认值 → YAML → 环境变量；`config validate` |
| 加密主密钥 | AES-256-GCM 密钥环；缺失/不匹配可拒绝启动 |
| 数据库 | Ent Schema + Atlas 迁移；SQLite 默认，PostgreSQL 可选 |
| 唯一管理员 | 环境变量引导或本机 setup 向导 |
| Session / CSRF | HttpOnly Session + 双提交 CSRF；登录限流 |
| 改密与 CLI 重置 | UI 改密；`admin reset-password --password-stdin` |
| 健康检查 | `/health/live`、`/health/ready` |
| 版本 API | `GET /api/v1/system/version`（需登录） |
| 前端认证壳 | 登录 / Setup / 主题切换 / 真实 readiness 仪表盘占位 |
| 生产嵌入 | Vite dist 可嵌入 Go 二进制（`production` tag） |

## 规格已定、尚未实现

完整设计见仓库内设计稿（开发用，默认不进文档站编译）：`docs/superpowers/specs/2026-07-26-reposentinel-design.md`。

| 阶段（规格 §23） | 内容 |
|------------------|------|
| 2 | GitHub App、Webhook Inbox、Issue/PR/Actions/安全事件规范化 |
| 3 | 自有仓同步、首次基线、Workflow 状态机、对账限流 |
| 4 | 外部公开仓轮询（≤20） |
| 5 | Rule Engine、聚合、Outbox、Telegram / HTTP Webhook |
| 6 | 完整管理后台（仓库/安全/通知/系统页） |
| 7 | Docker 镜像、GHCR、Changelog、发布流水线 |
| 8 | 备份 CLI、指标、安全加固与完整 E2E |

## 明确非目标（MVP）

- GitHub 个人通知收件箱同步
- 多用户 / RBAC / 多租户
- 在 Telegram 里写回 GitHub（关 Issue、合并 PR 等）
- GHES / 自定义 GitHub Base URL
- PR Review 评论类事件
- 超过 20 个外部仓的大规模轮询

## 下一步建议阅读

1. [快速开始](/guide/quick-start) — 本地跑通 Phase 1
2. [实现状态](/reference/implementation-status) — 对照规格的检查清单与验证证据
3. [配置参考](/reference/configuration) — 环境变量与密钥

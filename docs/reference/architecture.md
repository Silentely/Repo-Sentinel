# 系统架构

## 目标架构（完整产品）

```text
                    GitHub
       ┌──────────────┴──────────────┐
       │                             │
 GitHub App Webhook             GitHub REST API
 自有仓库实时事件          自有仓库对账 / 外部仓库轮询
       │                             ▲
       ▼                             │
┌────────────────────────────────────────────────────┐
│              HTTPS 反向代理                         │
└──────────────────────┬─────────────────────────────┘
                       ▼
┌────────────────────────────────────────────────────┐
│                RepoSentinel                         │
│ Webhook Receiver → Event Inbox → Normalizer        │
│ Scheduler → Reconciler / External Repo Poller      │
│ Event Store → Rule Engine / Daily Digest           │
│ Notification Outbox → Telegram / HTTP Webhook      │
│ REST API + 嵌入式 React 管理后台                    │
└──────────────────────┬─────────────────────────────┘
                       ▼
              SQLite 或 PostgreSQL
```

单进程模块化单体：HTTP Server、Inbox Worker、Scheduler、Sync Worker、Notification Worker、Cleanup Worker。异步任务持久化在数据库中。

## Phase 1 实际架构

```text
CLI (serve | version | config validate | admin reset-password)
        │
        ▼
   bootstrap / config / cryptox keyring
        │
        ▼
   store (Ent + Atlas 迁移) ── admin / session / audit / settings
        │
        ▼
   httpapi (chi)
        ├── /health/live|ready
        ├── /api/v1/setup/*
        ├── /api/v1/auth/*
        ├── /api/v1/system/version
        └── SPA (嵌入 dist 或 fallback)
```

**尚未接线的模块**（目录中亦不存在独立包）：Webhook、Normalizer、Reconciler、Poller、Rule Engine、Outbox、通知渠道适配器。

## 后端包边界（已实现）

| 包 | 职责 |
|----|------|
| `cmd/reposentinel` | 入口 |
| `internal/cli` | 命令分派 |
| `internal/config` | 加载与校验 |
| `internal/cryptox` | 主密钥与信封加密 |
| `internal/store` | 打开库、迁移、Admin/Session/Audit/Settings |
| `internal/auth` | 密码、Session、CSRF、登录限流 |
| `internal/httpapi` | 路由、中间件、JSON 错误、SPA |
| `internal/app` | 装配与生命周期 |
| `internal/buildinfo` | ldflags 版本元数据 |
| `web` | React 前端与 embed |

## 数据模型（Phase 1 表）

- `admin_accounts`（单例约束）
- `admin_sessions`
- `audit_logs`
- `system_settings`

Issue/PR、workflow_runs、security_alerts、events、outbox 等表属后续迁移。

## 前端结构（Phase 1）

- 认证：login / setup
- 壳层：侧栏占位、健康胶囊、主题
- 首页：readiness + 上手步骤（后续步骤明确标注未实现）
- 设计令牌：`web/src/styles/tokens.css`、暖调陶土强调

## 设计上下文

产品视觉与文案原则见仓库根目录 `.impeccable.md`（可能被 gitignore；与设计规格 §13 一致）。

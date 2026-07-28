# 系统架构

## 总体架构

```text
                    GitHub
       ┌──────────────┴──────────────┐
       │                             │
 GitHub App Webhook             GitHub REST API
 自有仓库实时事件          对账 / 外部仓扩展
       │                             ▲
       ▼                             │
┌────────────────────────────────────────────────────┐
│              HTTPS 反向代理                         │
└──────────────────────┬─────────────────────────────┘
                       ▼
┌────────────────────────────────────────────────────┐
│                RepoSentinel                         │
│ Webhook Receiver → Event Inbox → Normalizer        │
│ Event Store → Rule Engine                          │
│ Notification Outbox → Telegram / HTTP Webhook      │
│ REST API + 嵌入式 React 管理后台                    │
└──────────────────────┬─────────────────────────────┘
                       ▼
              SQLite 或 PostgreSQL
```

单进程模块化单体：HTTP Server、后台规范化、Notification Worker、Session 清理等。异步通知持久化在数据库中，进程重启可恢复投递。

## 运行时结构

```text
CLI (serve | version | config validate | admin reset-password)
        │
        ▼
   bootstrap / config / cryptox keyring
        │
        ▼
   store (Ent + Atlas 迁移)
        │
        ▼
   httpapi (chi)
        ├── /health/live|ready
        ├── /webhooks/github
        ├── /api/v1/setup|auth|...
        ├── /api/v1/dashboard|repositories|events|...
        └── SPA (嵌入 dist 或 fallback)
        │
        ▼
   notify worker (Outbox 投递)
```

## 后端包边界

| 包 | 职责 |
|----|------|
| `cmd/reposentinel` | 入口 |
| `internal/cli` | 命令分派 |
| `internal/config` | 加载与校验 |
| `internal/cryptox` | 主密钥与信封加密 |
| `internal/store` | 打开库、迁移、领域持久化 |
| `internal/auth` | 密码、Session、CSRF、登录限流 |
| `internal/githubx` | Webhook 验签等 |
| `internal/normalizer` | Webhook 规范化、指纹、乱序保护 |
| `internal/rules` | 实时通知规则 |
| `internal/notify` | Outbox 投递 Worker |
| `internal/httpapi` | 路由、中间件、JSON 错误、SPA |
| `internal/app` | 装配与生命周期 |
| `internal/buildinfo` | ldflags 版本元数据 |
| `web` | React 前端与 embed |

## 核心数据

- 管理员与 Session、审计、系统设置
- GitHub Installation、Repository（含同步/基线状态）
- Webhook Delivery、WorkItem、WorkflowRun、SecurityAlert、Event
- NotificationChannel、NotificationOutbox、SyncCursor

## 前端结构

- 认证：login / setup
- 壳层：侧栏、健康胶囊、主题
- 仪表盘：KPI、仓库与基线、事件、投递
- 通知：渠道配置
- 设计令牌：`web/src/styles/tokens.css`

## 设计上下文

产品视觉与文案原则见仓库根目录 `.impeccable.md`（若存在）及设计规格 §13。

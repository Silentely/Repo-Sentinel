# 架构设计

RepoSentinel 采用单进程模块化单体设计，强调**私有部署的高可用性**与**易维护性**。

## 顶层分层

```text
┌──────────────────────────────────────────────┐
│ GitHub Webhook / Polling / Reconcile         │
├──────────────────────────────────────────────┤
│ Event Normalizer & Fingerprinting            │
│ Event Store → Rule Engine                    │
│ Notification Outbox → Multi-Channel Delivery │
│ REST API + 嵌入式 React 管理后台             │
└──────────────────────┬───────────────────────┘
                       ▼
              SQLite 或 PostgreSQL
```

单进程模块化单体：HTTP Server、后台规范化、Notification Worker、Session 清理等。异步通知持久化在数据库中，进程重启可恢复传递。

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
   notify worker (Outbox 传递)
```

## 后端包边界

| 包 | 职责 |
|----|------|
| `cmd/reposentinel` | 入口 |
| `internal/cli` | 命令行分派 |
| `internal/config` | 加载与校验 |
| `internal/cryptox` | 主密钥与信封加密 |
| `internal/store` | 打开库、迁移、领域持久化 |
| `internal/auth` | 密码、Session、CSRF、登录限流 |
| `internal/githubx` | Webhook 验签、App Token 缓存与解耦、Manifest 交换等 |
| `internal/normalizer` | Webhook 规范化、指纹、乱序保护、仓库重命名自愈 |
| `internal/rules` | 实时通知规则、高危直推/强推守卫 |
| `internal/notify` | Outbox 传递 Worker、多渠道适配、短时聚合与死信 |
| `internal/syncx` | 周期对账、补偿通知管道、Star Release 追踪轮询 |
| `internal/ai` | AI 代码审查、Issue 分诊、CI 故障诊断与月度预算熔断 |
| `internal/botutil` | 机器账号识别与渠道免打扰判定 |
| `internal/chatops` | ChatOps 交互运维动作、令牌防重放与状态回写 |
| `internal/sideeffect` | 外部副作用执行收据与幂等原语 |
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
- 仪表盘：KPI、仓库与基线、事件、传递
- 通知：渠道配置
- UI 令牌：`web/src/styles/tokens.css`

## 视觉与文案

产品视觉与文案原则见仓库根目录 `.impeccable.md`（若存在）。

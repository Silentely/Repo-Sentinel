# store

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **store**

## 模块职责

持久化边界：打开 SQLite/PostgreSQL、执行 Atlas 迁移、定义领域模型与 Store 接口，经 Ent 实现仓储。业务包只依赖接口，不直接碰 Ent 生成代码。

## 入口与启动

- `store.Open(ctx, config.DatabaseConfig) (Store, error)` — 打开连接并迁移
- `migrate.go` — 应用嵌入的 `migrations` 方言目录
- `go generate ./internal/store/ent/...` — 由 schema 生成客户端

## 对外接口

`Store` 聚合（`store.go` + `domain.go`）：

| 方法 | 子接口 |
|------|--------|
| `Admins()` | 唯一管理员 |
| `Sessions()` | 会话 |
| `Settings()` | 系统设置 JSON |
| `Audits()` | 审计日志 |
| `Installations()` | GitHub App 安装 |
| `Repositories()` | 仓库与能力开关 |
| `WebhookDeliveries()` | Delivery 幂等 |
| `WorkItems()` | Issue/PR |
| `WorkflowRuns()` | Actions |
| `SecurityAlerts()` | 安全告警 |
| `Events()` | 规范化事件 |
| `Channels()` | 通知渠道 |
| `Outbox()` | 通知发件箱 |
| `Cursors()` | 同步游标 |
| `Dashboard` / `CleanupRetention` / `WithTx` / `Close` | 聚合与事务 |

共享领域工具：`RepoAllowsKind`、`NormalizeListFilter`、`IsFailureConclusion`、`IsSubscribableKind` 等。

## 关键依赖与配置

- Ent + `modernc.org/sqlite` / `pgx`
- 迁移 FS：`migrations.Dialect`
- 物理表名与 schema 注解一致（见下）

## 数据模型

### Ent schema → 表

| Schema 文件 | 表名 |
|-------------|------|
| `admin_account.go` | （管理员账号，见 schema） |
| `admin_session.go` | 管理员会话 |
| `audit_log.go` | 审计 |
| `system_setting.go` | 系统设置 |
| `github_installation.go` | `github_installations` |
| `repository.go` | `repositories` |
| `webhook_delivery.go` | `webhook_deliveries` |
| `work_item.go` | `work_items` |
| `workflow_run.go` | `workflow_runs` |
| `security_alert.go` | `security_alerts` |
| `event.go` | `events` |
| `notification_channel.go` | `notification_channels` |
| `notification_outbox.go` | `notification_outbox` |
| `sync_cursor.go` | `sync_cursors` |

### 关键常量

- 仓库类型：`github_installation` / `external_public`
- 同步状态：`baseline_sync` / `active` / `archived` / `unavailable`
- Outbox：`pending` / `sending` / `sent` / `dead`
- 渠道：`telegram` / `http_webhook`
- 外部仓上限：`MaxExternalRepositories = 20`

新增字段流程：schema → `go generate` → domain → store 实现 → fromEntity →（如需）Atlas 迁移。

## 测试与质量

- `domain_test.go`、`open_test.go`、`migrate_test.go`、`channel_subscription_test.go` 等
- `contract/contracts_test.go` — 契约测试
- 禁止手改 `ent/` 生成文件

## 常见问题 (FAQ)

**Q: GitHub ID 为什么必须 bigint？**  
A: 已超过 PostgreSQL int4 上限；见根 CLAUDE 迁移注意事项。

**Q: UpsertIfNewer 是什么语义？**  
A: 仅当来源更新时间/状态更新时写入，配合乱序 Webhook 丢弃陈旧数据。

## 相关文件清单

- `domain.go`、`store.go`、`domain_stores.go`、`open.go`、`migrate.go`
- `admin_store.go`、`session_store.go`、`settings_store.go`、`audit_store.go`、`adapter.go`
- `ent/schema/*.go`、`ent/generate.go`
- `feature_flags.go`、`errors.go`、`tx.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

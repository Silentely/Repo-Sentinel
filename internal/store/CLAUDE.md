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

**Q: 列表为什么要先 COUNT 再取页，不合并成一条查询？**  
A: `Total` 是分页契约的必需字段，前端据此算页数。合并需 `COUNT(*) OVER ()` 窗口查询，而本仓库 Ent 生成代码未提供 `Modify`，只能裸 SQL 重写 7 处列表方法（过滤条件、排序、归档排除都要复制一份，schema 漂移即错）。取证：SQLite 下 `events` 表 2 万行时「COUNT + 取页」平均 1.45ms（含 COUNT 与取页两次往返）；而事件表受 `retention.events_days`（默认 90 天）清理约束，规模有界。收益不抵风险，故维持两次查询。

**Q: 活跃仓 `RepositoryIDIn` 的大 IN 列表会不会撞绑定参数上限？**  
A: 会撞，但远超目标规模。modernc.org/sqlite 的 `SQLITE_MAX_VARIABLE_NUMBER` 实测为 32766（绑定 32767 个参数即报 `too many SQL variables`），需 3.2 万个活跃仓才会触发；外部仓另有 `MaxExternalRepositories = 20` 上限。取证：2000 个活跃仓时 WorkItems 列表平均 2.75ms，仍可接受。PostgreSQL 无此上限。因此不做 IN 分块或子查询改写，避免提前优化。

**Q: `notification_outbox.repository_full_name` 覆盖哪些历史行？**  
A: 只覆盖「写入时 `body_json` 已带 `kind=release` 与 `repository`」的行。该键自 2026-09-18 起由 `rules.Engine` 写入，迁移回填（`20260920001500_outbox_repository_full_name.sql`）也只从 `body_json` 取值；更早的旧形状行（`body_json` 无 `repository`）不再经关联事件回查，故不参与 unstar 取消。影响可忽略：投递失败最多重试 `maxAttempts = 8` 次（退避上限 1h）即转 dead，旧形状行在迁移执行时基本已终态，不存在长期滞留的 pending 行。若将来需要覆盖旧形状行，正确做法是新增迁移按 `event_id` 关联 `events.payload_summary->>'repository'` 回填（注意保持「非 release 类别不填充」的语义，且 `repository_full_name` 为 NOT NULL，子查询须有非空守卫）。

## 相关文件清单

- `domain.go`、`store.go`、`domain_stores.go`、`open.go`、`migrate.go`
- `admin_store.go`、`session_store.go`、`settings_store.go`、`audit_store.go`、`adapter.go`
- `ent/schema/*.go`、`ent/generate.go`
- `feature_flags.go`、`errors.go`、`tx.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-09-25T09:40:00Z | 优化 SplitFullName 边界与内存分配：改用 IndexByte 快速切分 owner 与 repo，消除 strings.SplitN 字符串切片堆分配；补齐表驱动单元测试 TestSplitFullName |
| 2026-09-20T00:00:00Z | 存储侧热点查询收敛：①`notification_outbox` 新增冗余列 `repository_full_name` 与 `(status, repository_full_name)` 索引，unstar 取消未投递 Release 通知改单条批量 UPDATE（不再逐户 SELECT→UPDATE）；②仪表盘统计同表维度改分组聚合（work_items 按 kind、repositories 按 sync_status 各一次扫描），整页刷新 SQL 往返减少；③活跃/归档仓 ID 集合缓存随附 `id→full_name` 映射，各资源列表页解析仓库名复用同一份扫描结果；④PR 审查在途判定改前缀快速拒绝，审查结果幂等查询由两次解析收敛为单条按 itemID 直查。补记：冗余列仅覆盖 `body_json` 带 `repository` 的行（该键自 2026-09-18 起写入），旧形状行不再回查关联事件，取证与补救方案见 FAQ |
| 2026-08-06T15:48:41Z | 新增 star 快照表、仓库 star/watch 能力开关（stars_enabled、watches_enabled）、feature.stars / feature.watches 全局开关及 star 快照读取与存储（含 SQLite/PostgreSQL 双轨迁移） |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

# migrations

[根目录](../CLAUDE.md) > **migrations**

## 模块职责

Atlas 版本化 SQL 迁移的双轨目录（SQLite + PostgreSQL），经 `embed.go` 嵌入二进制；Store 启动时按 driver 应用。校验和由 `go generate` 重算。

## 入口与启动

- `//go:embed sqlite/*.sql sqlite/atlas.sum postgres/*.sql postgres/atlas.sum`
- `Dialect("sqlite"|"postgres") (fs.FS, error)`
- `//go:generate go run ./tools/atlas-hash all` — 更新 `atlas.sum`
- 应用路径：`store.Open` → migrate

## 对外接口

仅供 `internal/store` 使用；开发者通过新增 SQL 文件扩展 schema。

## 关键依赖与配置

- 工具：`tools/atlas-hash/main.go`
- 根目录 `atlas.hcl`（如有本地 atlas 工作流）
- 与 Ent schema 物理表名必须一致

## 数据模型（迁移版本一览）

### 双轨共有（文件名对齐）

| 版本 ID | 说明 |
|---------|------|
| `20260727000100_initial` | 初始库 |
| `20260728000100_domain_core` | 领域核心表 |
| `20260729000100_repo_capability_toggles` | 仓库能力开关 |
| `20260729000200_workitem_review_checks` | PR review/checks 字段 |
| `20260729000300_item_ignored` | 本地忽略标记 |
| `20260730000200_channel_subscription` | 渠道订阅 kinds / digest |
| `20260731000100_missing_indexes` | 补索引 |
| `20260731000200_drop_scheduled_jobs` | 删除旧 scheduled_jobs |
| `20260731000400_drop_redundant_repository_type_index` | 删冗余索引 |

### 仅 PostgreSQL（类型/方言差异）

| 版本 ID | 说明 |
|---------|------|
| `20260729000400_workflow_run_bigint` | workflow run ID bigint |
| `20260730000100_github_id_bigint` | GitHub ID bigint |
| `20260731000300_unify_jsonb` | JSONB 统一 |
| `20260813000100_event_subject_number_bigint` | events.subject_number bigint（GitHub release ID 溢出防护） |

> 新增迁移：SQLite 与 PostgreSQL **语义同步**；仅 PG 需要的类型修正可只放 postgres，但需在评审中说明。

## 测试与质量

- `migrations_test.go` — 嵌入完整性/方言
- **禁止**手改已提交 SQL；**禁止**手写 `atlas.sum`
- 完整流程见根 CLAUDE「数据库迁移」

## 常见问题 (FAQ)

**Q: CI 报 atlas.sum 不匹配？**  
A: 运行 `go generate ./migrations/...` 后提交 sum。

**Q: 为什么 SQLite 少几个文件？**  
A: 部分变更为 PG 类型修正（int→bigint、jsonb），SQLite 无对应 DDL 需求。

## 相关文件清单

- `embed.go`、`migrations_test.go`
- `sqlite/*.sql`、`sqlite/atlas.sum`
- `postgres/*.sql`、`postgres/atlas.sum`
- `tools/atlas-hash/main.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

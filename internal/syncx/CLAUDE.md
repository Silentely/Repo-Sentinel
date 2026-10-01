# syncx

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **syncx**

## 模块职责

主动同步与调度：GitHub App 安装仓对账补漏、外部公开仓轮询、以及驱动 digest 的定时任务。

## 入口与启动

| 类型 | 说明 |
|------|------|
| `Reconciler` | 对账安装仓（分页拉取 Issue/PR/Actions/Alerts 等） |
| `ExternalPoller` | 外部公开仓轮询 |
| `Scheduler` | 定时触发上述任务 + digest 日/周/月 |
| `installations.go` | 安装与仓库列表同步辅助 |

`Scheduler` 默认周期：

- 启动延迟 ~45s
- ReconcileEvery = 6h（每轮最多若干仓，见 `ReconcileAll` limit）
- ExternalEvery = 10m
- DigestEvery = 1h

管理台可手动：`POST /api/v1/sync/reconcile`、单仓 reconcile、sync-repositories。

## 对外接口

- 被 `app` 持有并 `Run`
- 被 `httpapi` 注入 `Reconciler` 供手动触发
- 指标回调：`httpapi.MetricsIncReconcileRuns`

## 关键依赖与配置

- `githubx.AppClient` / `PublicClient`
- `store.Repositories.ListSyncCandidates`（按 last sync 避免饥饿）
- `digest.Generator`（可选 AI）

## 数据模型

更新仓库同步状态、WorkItem/WorkflowRun/SecurityAlert、SyncCursor；不拥有独立表定义。

## 测试与质量

- `reconcile_test.go`、`external_test.go`、`scheduler_test.go`

## 常见问题 (FAQ)

**Q: 为何不用 updated_at 选对账候选？**  
A: 刚同步的仓会插队导致其余仓饥饿；故用 `ListSyncCandidates`。

**Q: MaxPages 默认？**  
A: `app.Build` 设 `MaxPages: 3`，限制单次对账 API 消耗。

## 相关文件清单

- `reconcile.go`、`external.go`、`installations.go`、`scheduler.go`

## 变更记录 (Changelog)

| 日期 | 版本 / 范围 | 说明 |
|------|------------|------|
| 2026-09-28 | 缺陷修复 | 修复轮询收尾旧快照写回导致已删除仓库被复活的问题 |
| 2026-09-23 | 健壮性与并发 | 修复并发直呼 `PollOne` 时的字段竞态；对账读写失败不再静默忽略；规范归档状态收拢 |
| 2026-09-20 | 并发性能优化 | 外部公开仓轮询改有界并发，对账 PR 引入并发拉取；修复取消星标后未被移出的缺陷 |
| 2026-08-10 | 功能新增 | 新增 Star Release 轮询器，支持周期调度与增量 Release 事件化 |
| 2026-08-05 | 模块初始化 | 初始化模块 AI 上下文文档 |

> 完整历史变更请查阅根目录 [`CHANGELOG.md`](../../CHANGELOG.md)。

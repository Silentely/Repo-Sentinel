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

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-09-23T00:00:00Z | 归档收口重复实现收敛为 `collapseArchived`（`archived.go`）：对账 `ReconcileAll` 与外部轮询 `PollAll` 两条路径各自的 `UpdateSettings{IsArchived}` + `repo_state_update_failed` Warn 合并为单一辅助函数（`store`/`logger`/日志文案作参数），行为与日志内容不变 |
| 2026-09-23T00:00:00Z | star 同步用户名收敛写入与消费两侧边界：管理台 `PUT /api/v1/starred-releases/config` 对归一化后的用户名按 GitHub 字符集校验（`githubx.ValidGitHubUsername`，1-39 位字母/数字/连字符且不以连字符起止），非法值返回 400 `validation_failed` 并指明 `field=username`（此前含 `/`、空格的值会拼出错误 API 路径，star 同步每轮失败而用户侧无反馈）；`ListUserStarred` 路径构造改 `url.PathEscape` 兜底转义（历史脏值或其他写入路径漏校验时不再构成路径注入）；`syncStarsLocked` 对非法用户名跳过本轮、推进记账并 Warn 留痕 `star_sync_invalid_username`（与未配置同样避免每 1m 节拍空转）；补用户名字符集表驱动测试、含 `/` 用户名的线上转义路径断言、处理器 400 拒绝不覆盖已存合法值、轮询端跳过留痕四条回归 |
| 2026-09-23T00:00:00Z | 外部轮询/对账发现「GitHub 侧已归档但本地未联动」的仓时，顺手收口归档的 `UpdateSettings` 失败补 `repo_state_update_failed` Warn（此前 `_ =` 丢弃，本地仓会继续轮询并通知已归档仓）；`PollOne` 的客户端惰性初始化改只读回退到局部变量，导出的 `PollOne` 被并发直呼时不再构成 `p.Client` 同一字段的并发写，补 `-race` 回归测试 |
| 2026-09-23T00:00:00Z | 对账工作项读写失败不再静默吞掉：`UpsertIfNewer` 存储错误与「无变化/基线期」拆分为独立分支（`error_code=reconcile_upsert_failed` 留痕并计入软失败）；PR 旧行 `GetByRepoNumber` 读取错误显式区分 `store.ErrNotFound`，真实错误跳过该条（`error_code=work_item_read_failed`）且不再触发 enrich；存在读取/持久化失败时不推进 issues 游标，下轮从 since 重拉补齐（事件指纹幂等）；补写入失败与读取失败两条回归测试 |
| 2026-09-20T00:00:00Z | 外部公开仓轮询 `PollAll` 改有界并发（5 个 worker，`externalPollConcurrency`）：候选仓收集后按 worker 池分派，逐仓失败仅留 `external_poll_failed` 不影响其余仓，命中限流即停整轮并补 `rate_limited_round_stopped` Warn（`sync.Once` 保证一次），上下文取消立即返回；写入侧共享 `p.Client` 预先初始化，消除仓内懒赋值的并发写 |
| 2026-09-20T00:00:00Z | 对账 PR 补数据（评审/请求评审人/PR 详情）改固定 3 路并发扇出（非 worker 池，调用数固定），各 goroutine 只写局部变量、`wg.Wait` 后由主协程回填；单 PR 由 4 次串行 RTT 降为 2 轮（首轮并行 + 依赖 head SHA 的 Check Runs） |
| 2026-09-20T00:00:00Z | 修复 star 同步分页误判导致 unstar 移除被跳过：GitHub 多页结果末页仍带 Link 头（`rel="prev"/"first"`，无 `rel="next"`），越界空页同样非空，旧 `link == ""` 判断使分页打到页码防御上限并令 `full=false`，`removeUnstarred` 被整体静默跳过（已取消星标的仓继续被轮询推送）；改以 `rel="next"` 作为唯一翻页依据，页码上限命中补 Warn 留痕；新增 `LastStarSyncAt` 暴露最近一次完整同步落定时刻 |
| 2026-08-10T13:00:00Z | 新增 `StarredReleasePoller`：匿名枚举公开 star 仓库（fork/archived 预过滤、无 Release 7 天复查、unstar 自动停用、500 上限），复用 installation token + ETag 条件请求轮询最新 Release 并事件化（`kind=release`、`source=starred_releases`）；双周期从 system_settings 热读取（Star 同步默认 6h / Release 轮询默认 10m），由 Scheduler 1m 节拍驱动自判到期 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

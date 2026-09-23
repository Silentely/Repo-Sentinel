# normalizer

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **normalizer**

## 模块职责

将 GitHub Webhook JSON 载荷规范化为领域实体与 `Event`：仓库解析、Issue/PR/Actions/安全告警 upsert、去重指纹、乱序陈旧丢弃、基线期通知抑制。

## 入口与启动

- `Processor{Store}.Process(ctx, eventType, deliveryID, body) (Result, error)`
- `Result`：`Event`、`Repository`、`Updated`、`SuppressNotify`、`StaleDiscarded`、`UnhandledAction`

## 对外接口

被 `webhooksvc` 调用；不直接挂 HTTP。

主要处理事件类型（见 `process.go` 分发）：installation 生命周期、repository、issues、pull_request、workflow_run、各类 security alert 等。

## 关键依赖与配置

- `store` 各子仓储与 `RepoAllowsKind` 能力门禁
- 指纹：`fingerprint.go`
- 仓库解析：`repo.go`

## 数据模型

写入/更新：`Repository`、`WorkItem`、`WorkflowRun`、`SecurityAlert`、`Event`、`GitHubInstallation` 等。  
Delivery 行状态由 webhooksvc 维护，不在本包标记。

## 测试与质量

- `process_test.go`、`repo_test.go`

## 常见问题 (FAQ)

**Q: 什么是 SuppressNotify？**  
A: 基线同步或归档等场景抑制通知洪流，事件仍可入库。

**Q: StaleDiscarded？**  
A: 来源更新时间旧于库内状态时丢弃写入，防止乱序 Webhook 回退状态。

## 相关文件清单

- `process.go`、`fingerprint.go`、`repo.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-09-23T00:00:00Z | PR 合并置位（`MarkMerged`）改用本次解析出的 `repo.ID`，不再解引用 `*res.Event.RepositoryID`：事件行的仓库关联并非置位标记的前提，指针解引用只带来空指针风险——一旦事件行缺 `repository_id`，一条正常的 PR 合并 webhook 会 panic 致整个处理失败；补事件行缺仓库关联时合并标记仍落定的回归（装饰存储注入 nil RepositoryID，先还原实现验证可捕获 panic） |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

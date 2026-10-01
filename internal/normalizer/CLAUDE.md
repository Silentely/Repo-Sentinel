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

| 日期 | 版本 / 范围 | 说明 |
|------|------------|------|
| 2026-09-28 | 缺陷修复 | 修复 Webhook 事件首建仓库缺失安装绑定问题；防止外部公开仓被普通事件误改判为安装仓 |
| 2026-09-25 | 性能优化 | 优化仓库名称切分与事件指纹生成性能 |
| 2026-09-23 | 稳定性修复 | 修复 PR 合并事件在缺失仓库关联时可能发生的空指针异常 |
| 2026-08-05 | 模块初始化 | 初始化模块 AI 上下文文档 |

> 完整历史变更请查阅根目录 [`CHANGELOG.md`](../../CHANGELOG.md)。

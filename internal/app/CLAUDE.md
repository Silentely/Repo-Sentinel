# app

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **app**

## 模块职责

应用装配与进程生命周期：打开数据库、校验加密、注入 GitHub/AI/通知/调度依赖、启动 HTTP 与后台 goroutine，并在取消时优雅关闭。

## 入口与启动

| 符号 | 文件 | 说明 |
|------|------|------|
| `Build` | `bootstrap.go` | 按配置装配 `*App` |
| `Run` | `run.go` | ListenAndServe + Worker/Scheduler/清理 |
| `Close` | `app.go` | 幂等关闭 DB 与 worker context |
| `SupportedSchemaVersion` | `app.go` | 当前二进制声明的 schema 版本常量 |

后台任务（`Run`）：

1. Session 清理（`defaultCleanupInterval` = 15m）
2. 保留策略清理（24h）
3. `notify.Worker`（5s）
4. `syncx.Scheduler`（对账/外部/摘要）
5. HTTP Server

关闭预算：`gracefulShutdownTimeout` = 30s。

## 对外接口

- 对 CLI：`Build` / `Run` / `Close` / `ResetAdminPassword` / `PublicError`
- 对 HTTP：通过 `httpapi.Dependencies` 注入，不直接导出路由

## 关键依赖与配置

依赖包：`config`、`store`、`cryptox`、`auth`、`githubx`、`httpapi`、`rules`、`notify`、`syncx`、`digest`、`ai`、`updatecheck`、`buildinfo`、`web`（前端 FS）。

引导逻辑：

- `bootstrapConfiguredAdmin`：配置文件中的初始管理员
- `bootstrapNotifyChannels`：环境/配置中的 Telegram、HTTP Webhook 渠道
- GitHub/AI 运行时：`env 基线 + DB MergeFromStore`

可测试注入：`buildDependencies`（openStore、validateEncryption、openFrontend、newHTTPServer 等）。

## 数据模型

不拥有表；读写经 `store.Store`。保留天数键：

- `retention.events_days`
- `retention.outbox_days`
- `retention.webhook_deliveries_days`

## 测试与质量

- `bootstrap_test.go`：装配与错误映射
- 失败路径必须取消 `workerCtx`，避免泄漏

## 常见问题 (FAQ)

**Q: SupportedSchemaVersion 与最新迁移不一致？**  
A: 该常量用于就绪/版本展示侧的「支持版本」声明；实际迁移由 Atlas 嵌入目录驱动。改 schema 后需同步评估是否更新此常量。

**Q: 加密密钥不匹配如何表现？**  
A: 返回 `PublicError` 码 `encryption_key_mismatch`，CLI 只打印安全文案。

## 相关文件清单

- `app.go` — App 结构与 PublicError
- `bootstrap.go` — Build 装配
- `run.go` — Run 与清理循环
- `encryption.go` — 主密钥校验
- `github_runtime.go` — 从 env 构造 GitHub Runtime

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

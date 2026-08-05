# rules

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **rules**

## 模块职责

实时通知决策：根据规范化结果判断是否通知、格式化正文、写入 Outbox；`Aggregator` 在短窗口内合并同仓同类事件并处理超频摘要。

## 入口与启动

- `Engine.Evaluate(ctx, res, repoFullName)` — 单事件决策
- `NewAggregator(store, window, burstN, burstW)` — 进程内合并；多实例依赖 Outbox 幂等键 best-effort

默认窗口（`app.Build` 与 config）：Window 60s、BurstThreshold 15、BurstWindow 5m。

## 对外接口

- 实现 `webhooksvc.Evaluator`
- 可选 `AI *ai.Client`：安全告警分诊（超时 `aiTriageTimeout` = 15s，失败则无分诊正文）

门禁：

- 全局/仓库能力开关（`store.RepoAllowsKind` 等）
- `shouldNotifyRealtime` 过滤噪音 action
- 渠道 `EventKinds` 订阅（写入 Outbox 时按渠道展开）

## 关键依赖与配置

- `config.Aggregation`
- `store.Channels` / `Outbox`
- ULID 生成幂等键/ID

## 数据模型

创建 `NotificationOutbox` 行（pending）；不直接发 HTTP。

## 测试与质量

- `engine_test.go`、`engine_filter_test.go`、`aggregate_test.go`、`formatter_test.go`

## 常见问题 (FAQ)

**Q: Aggregator 多副本会重复通知吗？**  
A: 进程内合并非分布式；跨实例靠 Outbox `IdempotencyKey` 去重，仍为 best-effort。

**Q: 失败类 Actions 如何判定？**  
A: 使用 `store.IsFailureConclusion` 单一来源。

## 相关文件清单

- `engine.go`、`aggregate.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

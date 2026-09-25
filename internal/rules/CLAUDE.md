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
- 可选 `AI *ai.Client`：安全告警分诊与 release 总结（等待时长 = 配置的请求超时，失败则无 AI 段落）
- 可选 `Logger *slog.Logger`：分诊参与度留痕，由 `app.Build` / `webhooksvc` 注入

## 分诊日志留痕

`Engine.triageAnalysis` 输出参与度日志（Logger 注入时），与 ai 层 `ai request ok/failed` 配合还原调用链：

- `INFO triage ai skipped` — 未参与，reason 区分 `triage_not_enabled` / `not_new_alert` / `no_subscribed_channel`（无订阅渠道时同样不发 AI 请求，避免无效费用）
- `INFO triage ai used` — 分诊成功，附 duration_ms
- `WARN triage ai fallback` — 失败回退（reason=`ai_error` / `empty_analysis`，附错误详情），正文保持原文

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
| 2026-09-25T11:05:00Z | 优化消息聚合与超频降级消息组装：renderMergedMessage 与 enqueueBurstSummary 消除全部 fmt.Sprintf 反射格式化装箱，改用原生拼接与 strconv，完全移除 rules 包对 fmt 依赖 |
| 2026-09-25T10:00:00Z | 优化 renderMessage 纯文本标题组装：使用原生字符串拼接替代 fmt.Sprintf，消除高频事件流中的格式化解析与 interface 逃逸装箱开销 |
| 2026-09-25T08:52:00Z | 优化规则引擎状态映射与幂等键生成开销：EventStatusLabel 委托 statusDisplay 实现单点维护并消除冗余分支；idempotencyKey 改用 io.WriteString 与栈缓冲区十六进制编码消除中间切片分配；ReloadFrom 采用 errors.Join 严格传递存储层错误 |
| 2026-09-25T07:45:00Z | 通知标题纯文本与正文 HTML 转义安全收口：`renderMessage` 与 `renderMergedMessage` 返回纯文本标题（供 Outbox.Title 及钉钉/飞书/企微/Slack 标题消费，杜绝 `&lt;`、`&amp;` 等实体泄漏），并在 HTML 正文的 `<b>` 标签中统一使用 `htmlpkg.EscapeString` 转义，兼顾各渠道展示美观与 Telegram 解析安全 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

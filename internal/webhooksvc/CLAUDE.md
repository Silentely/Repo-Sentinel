# webhooksvc

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **webhooksvc**

## 模块职责

GitHub Webhook **业务管线**（与 HTTP 解耦）：规范化 → 实时通知评估 → `WebhookDelivery` 状态机标记。使 `httpapi` 仅做请求适配与验签入库。

## 入口与启动

- `Service.Process(rowID, eventType, deliveryID, body)`  
  在 `Background` context 上运行；关闭时用 `WithoutCancel` + 超时做状态标记，避免永久 `accepted`。

装配：`httpapi.New` 内创建 `webhooksvc.Service`，注入 Store、Logger、Aggregator（Evaluator）、AI、Background。

## 对外接口

- `Evaluator` 接口：`Evaluate(ctx, normalizer.Result, repoFullName)`  
  默认实现为 `rules.Aggregator`；nil 时回退 `rules.Engine`。

状态：

- 规范化失败 → `DeliveryFailed` / `normalize_failed`
- 规则失败 → `DeliveryFailed` / `rule_failed`
- 成功 → `DeliveryProcessed`

## 关键依赖与配置

- `normalizer.Processor`
- `rules`（Engine / Aggregator）
- 可选 `ai.Client`（安全告警分诊透传）

## 数据模型

读写 `store.WebhookDeliveries`、经 normalizer 写入 WorkItem/Event 等。

## 测试与质量

- 当前以集成路径为主（httpapi webhook 测试覆盖）；包文件精简为 `service.go`

## 常见问题 (FAQ)

**Q: 为何从 httpapi 抽出？**  
A: 避免 HTTP 层编排领域流程；后台 panic/取消语义与请求生命周期分离。

**Q: SuppressNotify 时还标记 processed 吗？**  
A: 规范化成功且通知被抑制时仍标记 processed（不 Evaluate）。

## 相关文件清单

- `service.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

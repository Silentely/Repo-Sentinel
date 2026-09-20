# webhooksvc

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **webhooksvc**

## 模块职责

GitHub Webhook **业务管线**（与 HTTP 解耦）：规范化 → 实时通知评估 → `WebhookDelivery` 状态机标记。使 `httpapi` 仅做请求适配与验签入库。另承载 PR AI 代码审查的编排入口（`code_review.go`）：webhook 自动触发与管理台手动触发共用同一条「拉 Diff → LLM 审计 → 持久化 → 可选评论回写 → 高危预警」管线。

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

- `service_test.go`、`code_review_test.go`：Webhook 管线状态机与 PR 审查双触发路径
- `review_tracker_test.go`、`service_budget_test.go`：在途去重、等待与取消、处理预算
- `code_review_internal_test.go`（同包）：安装 ID 解析四条路径、审查结果幂等判定的边界

## 常见问题 (FAQ)

**Q: 为何从 httpapi 抽出？**  
A: 避免 HTTP 层编排领域流程；后台 panic/取消语义与请求生命周期分离。

**Q: SuppressNotify 时还标记 processed 吗？**  
A: 规范化成功且通知被抑制时仍标记 processed（不 Evaluate）。

**Q: 同一 PR 重放 webhook、重复点击手动触发会重复审查吗？**  
A: 不会。在途判定以 `<full_name>#<pr>#<headSHA>` 为键抢占（`reviewTracker.acquire`），命中即拒绝登记；手动触发另以 `<full_name>#<pr>#` 前缀快速拒绝，只要该 PR 有审查在途（**不限 head SHA**）即返回 `ai_review_in_progress`，省掉一次注定无法并行的令牌解析与 `GetPRDetail`。该前缀比 SHA 粒度更宽是有意的：同 PR 换 commit 后并发审查会重复消耗 AI 配额，并争抢同一 `ai.pr_review.<itemID>` 挂载点（后写覆盖先写）；代价是新 head SHA 的手动触发需等在途任务结束后重试。此外自动路径同一 head SHA 已有审查结果时 `skipStored` 直接跳过；手动触发为强制刷新，恒为 false。

## 相关文件清单

- `service.go` — Process 管线与状态机
- `code_review.go` — PR 代码审查编排、安装 ID 解析、结果持久化
- `review_tracker.go` — 审查在途去重（前缀快速拒绝 + 等待/停止）

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-09-20T00:00:00Z | PR 审查去重与查询收敛：手动触发前置按 `<full_name>#<pr>#` 前缀拒绝同 PR 在途审查（不限 head SHA，避免同 PR 多 commit 并发审查重复消耗配额并争抢同一挂载点），webhook 重试不重复占用 AI 配额；审查结果幂等查询由「GetByFullName → GetByRepoNumber → 读设置」三级解析收敛为按 work item ID 单条直查；resolveRepoInstallationID 补单 App 部署回退与 Warn 留痕 |
| 2026-09-16T00:00:00Z | PR 打开/同步异步触发 AI 代码审查；规则引擎评估前新增订阅渠道检查，无启用渠道跳过通知与分诊；新增 Service.MarkFailed 显式标记并发槽位获取失败的投递，避免行残留 accepted；CI 失败诊断事件类型门控前置于 AI 开关检查；摘要生成器活动仓不超过 3 个时主键点查；超频滑动窗口超 100 键自动清理 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

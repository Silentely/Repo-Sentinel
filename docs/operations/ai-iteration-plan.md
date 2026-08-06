# AI 链路打磨迭代计划（头脑风暴）

> 面向 RepoSentinel 的 AI 能力（摘要 / 安全告警分诊 / 连通性测试）持续打磨清单。
> 基线：AI 调用日志可观测已落地（`ai request start/ok/failed` + error_code 分类 + digest/rules 参与度三段日志）。
> 本文记录 10 个候选打磨项：问题 → 方案 → 收益 → 成本 → 风险，供分批实施。

## 实施状态

| 批次 | 项 | 状态 |
|------|----|------|
| 1 | #1 指标、#2 token 用量、#3 请求关联 ID、#8 并发预算、#9 质量护栏 | ✅ 已完成（2026-08-06） |
| 2 | #5 瞬时重试、#6 输入去重缓存、#10 温度参数化 | ⬜ 待实施 |
| 3 | #4 运行状态端点+前端、#7 模型回退链 | ⬜ 待实施 |

## 现状锚点（代码位点）

| 位点 | 说明 |
|------|------|
| `internal/ai/client.go` `Complete` | 所有 LLM 调用的唯一出口；已有统一留痕与错误分类（timeout/network/upstream_\<status\>/bad_response/empty_response/internal） |
| `internal/ai/summarize.go` | `SummarizeEvents` / `TriageAlert` 两个业务入口；system prompt 与 Temperature(0.3) 硬编码 |
| `internal/digest/generator.go` `reportBody` | 摘要 AI 参与度：used / skipped / fallback |
| `internal/rules/engine.go` `triageAnalysis` | 分诊 AI 参与度：used / skipped / fallback；15s 独立预算 |
| `internal/httpapi/metrics.go` | 进程内 atomic 计数器 + `/metrics` Prometheus 文本暴露（webhook/outbox/reconcile） |
| `internal/httpapi/ai_config_handlers.go` | GET/PUT/Test AI 配置；探测用独立 HTTP client |
| `internal/ai/runtime.go` | env 基线 + DB 补缺、热更新（`RuntimeConfig`） |
| `web/src/features/monitor/about-page.tsx` | 管理台 AI 配置区（保存 / 连通性测试） |

## 10 个打磨项

### A 类：可观测性补强（延续日志留痕主线）

#### 1. AI 调用指标（Prometheus 计数器）

- **问题**：日志能回答成败，但 `/metrics` 看不到 AI 成功率、延迟、用量，无法接入现有监控面板。
- **方案**：沿用 `httpapi/metrics.go` 的 atomic 计数器模式，在 `Complete` 出口统一计数：`reposentinel_ai_requests_total`、`reposentinel_ai_requests_failed_total`（按 error_code 分列，或单计数 + 标签）、`reposentinel_ai_request_duration_ms`（累计总和 + 次数，供均值）、token 计数（见 #2）。在 `handleMetrics` 输出。
- **收益**：成功率 / 延迟 / 成本直接上监控，故障先于用户发现。
- **成本**：低（纯增量，模式现成，不碰配置与 DB）。
- **风险**：无（进程内计数，多副本各自独立，与既有指标语义一致）。

#### 2. Token 用量追踪与记账

- **问题**：BYOK 用户看不到每次调用花多少 token；OpenAI 兼容响应携带 `usage` 字段（prompt/completion/total），当前 `chatResponse` 直接丢弃。
- **方案**：`chatResponse` 增加 `usage` 解析；成功日志 `ai request ok` 附 `prompt_tokens` / `completion_tokens` / `total_tokens`；指标累计 `reposentinel_ai_prompt_tokens_total` / `reposentinel_ai_completion_tokens_total`。
- **收益**：成本可观测；日志可直接还原单次开销。
- **成本**：低。
- **风险**：部分网关不返回 usage → 缺省为 0，不影响调用。

#### 3. 请求关联 ID（端到端串联）

- **问题**：`digest ai used` 与 `ai request ok` 是相邻两行但无共同键；并发调用时无法按 ID 串联「参与度 → 调用 → 结果」。
- **方案**：`Complete` 增加可选 `reqID`（context 携带或参数），缺省生成短 ULID；digest / rules 参与度日志与 ai 层调用日志均带 `req_id` 字段。
- **收益**：可归因性闭环——按一次 AI 决策可串联全部日志与指标。
- **成本**：低中（digest/rules 两处调用点透传）。
- **风险**：日志字段增加，需同步 CLAUDE.md 约定与测试断言。

#### 4. AI 运行状态端点 + 管理台展示

- **问题**：管理台只有「测试连通性」，看不到历史调用情况；排障必须翻服务端日志。
- **方案**：`httpapi` 新增只读 `GET /api/v1/ai/stats`：进程内环形缓冲最近 N（如 50）次调用（时间 / 模型 / 耗时 / error_code / ok）+ 累计计数；前端 AI 配置区下方展示最近调用列表。
- **收益**：管理台直接可视，运维零门槛。
- **成本**：中（新 handler + 前端组件 + 测试）。
- **风险**：内存环形缓冲上限需固定，避免膨胀。

### B 类：稳定性与成本

#### 5. 瞬时错误有限重试（指数退避 + Retry-After）

- **问题**：网关 5xx / 网络抖动 / 429 会让摘要或分诊直接降级；多数瞬时错误重试一次即可成功。
- **方案**：`Complete` 对 `network` / `upstream_429/500/502/503` 重试（默认 2 次，指数退避 500ms 起）；429 优先尊重 `Retry-After` 头；日志标注 `attempt=N`；总预算受 timeout 约束。重试预算需计入 digest 调度窗口与分诊 15s 预算。
- **收益**：降级率显著下降；429 限流语义正确处理。
- **成本**：中（重试循环 + 预算交互测试）。
- **风险**：重试放大延迟与费用 → 必须限定次数与退避上限，且只对可重试分类生效。

#### 6. 摘要输入去重缓存

- **问题**：同一窗口事件（幂等重跑、日/周窗口重叠）会对相同输入重复调用 AI，重复付费。
- **方案**：以「事件 ID 有序哈希 + period + model」为键的内存缓存（TTL 1h、容量上限 100）；命中直接复用输出；事件集合变化即 miss。
- **收益**：省 token；幂等重跑零成本。
- **成本**：中（缓存键必须覆盖全部影响输出的输入，正确性边界需测试）。
- **风险**：缓存失效策略错误会投递过期摘要 → 键必须含完整输入语义，TTL 保守。

#### 7. 模型回退链（fallback model）

- **问题**：主模型 / 主网关宕机时全部 AI 功能降级，用户只能手动改配置。
- **方案**：config / DB 增加 `fallback_model`（可选）；`upstream_5xx` / `429` / `network`（重试仍失败后）自动用 fallback_model 再试一次；日志标注模型切换。
- **收益**：上游故障时摘要 / 分诊仍可用。
- **成本**：中高（config 结构、DB 存储、UI 校验、测试均需动；走 system settings JSON，不涉及 schema 迁移）。
- **风险**：fallback 模型质量差异 → 仅作为降级路径，成功路径仍用主模型。

#### 8. AI 调用并发预算（信号量）

- **问题**：digest 三周期与分诊可能并发打 AI；小模型网关并发敏感，突发调用推高延迟与费用。
- **方案**：`ai.Client` 增加全局信号量（默认并发 1~2，可配置）；超出时排队（带超时）或直接降级（fallback 语义，日志标注 `reason=concurrency_limit`）。
- **收益**：费用与延迟可控，网关不被突发打爆。
- **成本**：低。
- **风险**：排队可能加剧超时 → 队列超时上限需小于各调用方预算。

### C 类：输出质量与可调性

#### 9. 摘要输出质量护栏

- **问题**：AI 输出非空但可能过短（如只回「无事件」）、与模板雷同（复读分组文本）、或被提示注入带出异常内容。
- **方案**：`reportBody` 增加质量判定：长度下限（如 20 字）、与模板正文的相似度阈值（包含/重合检测），不达标走 fallback（`reason=low_quality`）；triage 校验「影响：」前缀，缺失则降级。
- **收益**：投递质量稳定，防止「AI 输出反而更差」。
- **成本**：低中。
- **风险**：误杀合法简短输出 → 阈值需保守，宁可多回退。

#### 10. 提示词与温度参数化

- **问题**：Temperature 0.3 与 system prompt 硬编码在源码，用户无法微调风格 / 确定性；多网关行为差异无法适配。
- **方案**：config / DB 增加 `Temperature`（可编辑，默认 0.3，校验范围 [0,2]）；prompt 常量集中管理并预留配置覆盖入口（本期至少落地 temperature）。
- **收益**：用户可调确定性 / 创造性；适配不同网关。
- **成本**：低中（config + DB + UI + 测试）。
- **风险**：无（范围校验兜底）。

## 建议实施批次

| 批次 | 项 | 主题 | 预估规模 |
|------|----|------|----------|
| 1 | #1 #2 #3 #8 #9 | 可观测 + 稳定性低垂果实 | 中（纯 Go 增量，不动配置面） |
| 2 | #5 #6 #10 | 稳定性与可调性 | 中（含 config/DB 面） |
| 3 | #4 #7 | 管理台可视 + 高可用 | 中大（含前端与配置面） |

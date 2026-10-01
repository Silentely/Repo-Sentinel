# ai

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **ai**

## 模块职责

可选 LLM 集成（OpenAI 兼容 Chat Completions）：客户端、运行时配置合并、摘要与安全告警分诊文案生成。默认关闭，失败 soft-fail。

## 入口与启动

- `RuntimeFromEnv(cfg.AI)` + `MergeFromStore` — 与 GitHub 类似的 env/DB 合并
- `RuntimeConfig.Client()` → `*Client`
- `summarize.go` — 摘要/分诊提示与解析

由 `app.Build` 注入：`rules.Aggregator.AI`、`digest.Generator.AI`、`httpapi`/`webhooksvc`。

## 对外接口

- HTTP：`GET/PUT /api/v1/ai/config`（掩码）
- 库接口：Client 完成 chat completion；上层设置 Timeout/MaxTokens/Retries

配置字段（`config.AIConfig`）：Enabled、BaseURL、APIKey、Model、Timeout、MaxTokens、Retries、DigestEnabled、TriageEnabled、ReleaseSummaryEnabled、CodeReviewEnabled、CodeReviewCommentOnPR、FailureAnalysisEnabled。

## 重要依赖与配置

- 环境变量 `REPOSENTINEL_AI_API_KEY`、`REPOSENTINEL_AI_RETRIES`（默认 1，范围 0–5）
- 密钥可信封存 DB（经 KeyRing）
- 默认模型倾向 `gpt-4o-mini` 类小模型（见 defaults）

## 数据模型

无业务表；配置落在 system settings。

## 日志留痕

所有 LLM 调用统一经 `Client.Complete` 留痕（注入 Logger 时；未配置不发起请求也不留痕），
日志均携带 `req_id`（context 未注入时自动生成，digest/rules 层经 `EnsureRequestID` 注入后
参与度日志与调用日志共用同一 ID，可按单次决策端到端串联）：

- `DEBUG ai request start` — 发起请求：req_id、model、endpoint（URL 的 userinfo 段已打码，防内嵌凭据泄出）、max_tokens、timeout_ms、input_bytes、retries
- `DEBUG ai request retry` — 瞬时失败后重试：req_id、model、attempt、error_code、error、delay_ms
- `INFO ai request ok` — 成功：req_id、model、duration_ms、output_chars、prompt_tokens、completion_tokens
- `WARN ai request failed` — 最终失败：error_code 分类 + error 详情

`error_code` 区分故障来源：`timeout`（超时）、`network`（网络层失败）、`upstream_<status>`（上游非 2xx，error 含响应体明细）、`bad_response`（响应解码失败）、`empty_response`（无内容）、`concurrency_limit`（并发预算排队超预算）、`internal`。上层（如 digest 的 `digest ai used / ai skipped / ai fallback`、rules 的 `triage ai used / ai skipped / ai fallback`）记录 AI 参与度，与上述调用日志配合可还原完整链路。

## 重试策略

瞬时失败（`timeout` / `network` / `upstream_5xx` / `empty_response` / `bad_response`）按 `Retries` 配置自动重试，每次重试前等待 `retryDelay`（默认 1s）；`upstream_4xx`（确定性错误）与 `concurrency_limit` / `internal` 不重试。重试受外层 context 预算约束（分诊/release 按配置超时建预算，到期即放弃），digest 无硬限时总时长 ≈ 超时 × (1+重试次数)。指标只计最终结果，耗时含全部尝试。请求超时一律以配置 `timeout` 为准：包级默认 HTTP 客户端不带 Timeout 硬顶，外层预算由调用方（rules 的 `EffectiveTimeout`、webhooksvc 的 `processBudget`）按配置派生，不再有硬编码上限截断。

## 指标与运行时行为

- 指标与日志同源，在 `Complete` 出口统一累计（`metrics.go`）：请求/失败/耗时/token，失败按 error_code 分列；由 httpapi `/metrics` 以 `reposentinel_ai_*` 暴露。
- 并发预算：同一客户端实例在途 LLM 请求上限 `aiMaxConcurrency`（默认 2），超出排队（等待计入总时长，受调用方 ctx 预算约束），排队超预算以 `concurrency_limit` 降级。
- 质量护栏：digest 侧对过短或复读模板的输出回退（`reason=low_quality`）；rules 侧校验首行「影响：」前缀（`reason=format_invalid`）。

## 测试与质量

- `client_test.go`、`runtime_test.go`、`summarize_test.go`

## 常见问题 (FAQ)

**Q: AI 慢是否阻塞 Webhook？**  
A: 分诊等待时长 = 配置的请求超时（rules 侧按 `EffectiveTimeout` 建预算）；超时则通知不含 AI 段落。

**Q: 可接本地模型吗？**  
A: 可以，将 BaseURL 指向 OpenAI 兼容网关即可。

## 相关文件清单

- `client.go`、`runtime.go`、`summarize.go`

## 变更记录 (Changelog)

| 日期 | 版本 / 范围 | 说明 |
|------|------------|------|
| 2026-09-25 | 性能优化 | 优化 Diff 切片解析与换行规范化；优化 Prompt 与 Markdown 格式化内存分配 |
| 2026-09-23 | 稳定性优化 | AI 请求重试耗尽时增加尝试次数标识，区分偶发抖动与持续故障 |
| 2026-09-16 | 功能新增 | 新增 PR Diff 智能代码审查与 Actions CI 失败根因诊断能力；支持手动审查入队与结果回写 |
| 2026-08-10 | 功能扩展 | 新增 Star 仓库 Release 更新中文总结，支持重试机制与调用指标统计 |
| 2026-08-05 | 模块初始化 | 初始化模块 AI 上下文文档 |

> 完整历史变更请查阅根目录 [`CHANGELOG.md`](../../CHANGELOG.md)。

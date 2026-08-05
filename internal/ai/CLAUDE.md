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
- 库接口：Client 完成 chat completion；上层设置 Timeout/MaxTokens

配置字段（`config.AIConfig`）：Enabled、BaseURL、APIKey、Model、Timeout、MaxTokens、DigestEnabled、TriageEnabled。

## 重要依赖与配置

- 环境变量 `REPOSENTINEL_AI_API_KEY`
- 密钥可信封存 DB（经 KeyRing）
- 默认模型倾向 `gpt-4o-mini` 类小模型（见 defaults）

## 数据模型

无业务表；配置落在 system settings。

## 测试与质量

- `client_test.go`、`runtime_test.go`、`summarize_test.go`

## 常见问题 (FAQ)

**Q: AI 慢是否阻塞 Webhook？**  
A: 分诊有独立超时（rules 侧 15s）；超时则通知不含 AI 段落。

**Q: 可接本地模型吗？**  
A: 可以，将 BaseURL 指向 OpenAI 兼容网关即可。

## 相关文件清单

- `client.go`、`runtime.go`、`summarize.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

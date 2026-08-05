# githubx

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **githubx**

## 模块职责

GitHub 集成适配层：App JWT 与 Installation Token、Webhook 路径与 HMAC 验签、REST 辅助、可热更新的 Runtime 配置（env 优先，DB 补缺）。

## 入口与启动

- `WebhookPath = "/webhooks/github"` — 路径唯一事实来源
- `NewAppClient(appID, privateKeyPath)` — App 客户端
- `PublicClient` — 外部公开仓（PAT）
- `RuntimeConfig` + `MergeFromStore` — 管理台可编辑配置合并
- `signature` — 入站 Webhook 验签（支持 previous secret 轮换）

## 对外接口

- 对 httpapi：验签、配置读写、安装/仓库同步所需客户端方法
- 对 syncx：对账与外部轮询使用的 GitHub API 调用
- 设置键与掩码逻辑见 `settings.go`

## 关键依赖与配置

配置段 `github` + 环境变量（Webhook Secret、External PAT 等）。  
私钥文件路径在配置中，内容不入库。

## 数据模型

- 安装与仓库状态落在 `store`；本包不拥有表
- 运行时密钥可经 `cryptox` 信封写入 system settings

## 测试与质量

- `app_test.go`、`rest_test.go`、`signature_test.go`、`runtime_test.go`、`settings_test.go`

## 常见问题 (FAQ)

**Q: Webhook 路径能否改？**  
A: 应只改 `WebhookPath` 常量，路由与文档均依赖它。

**Q: env 与 DB 冲突时谁优先？**  
A: 设计为 env 基线、DB 补缺；具体字段合并见 `MergeFromStore` / Runtime 实现。

## 相关文件清单

- `app.go`、`rest.go`、`runtime.go`、`settings.go`、`signature.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

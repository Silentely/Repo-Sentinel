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

| 日期 | 版本 / 范围 | 说明 |
|------|------------|------|
| 2026-09-25 | 性能优化 | 优化 Webhook 签名校验与用户名合法性检查性能 |
| 2026-09-23 | 缺陷与稳定性修复 | 修复 Star 同步用户名字符集校验与转义防注入；修复运行时信封解密失败静默降级问题 |
| 2026-09-20 | 缺陷修复 | 修正 Star 列表分页依据为 `rel="next"`，修复取消星标后未被移出的缺陷 |
| 2026-09-16 | 功能扩展 | 新增 PR Diff 拉取与 Issue 评论创建接口；支持 Actions 任务分页拉取与连接复用 |
| 2026-08-05 | 模块初始化 | 初始化模块 AI 上下文文档 |

> 完整历史变更请查阅根目录 [`CHANGELOG.md`](../../CHANGELOG.md)。

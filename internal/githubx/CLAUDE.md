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
| 2026-09-23T00:00:00Z | star 同步用户名收敛写入与消费两侧边界：管理台 `PUT /api/v1/starred-releases/config` 对归一化后的用户名按 GitHub 字符集校验（`githubx.ValidGitHubUsername`，1-39 位字母/数字/连字符且不以连字符起止），非法值返回 400 `validation_failed` 并指明 `field=username`（此前含 `/`、空格的值会拼出错误 API 路径，star 同步每轮失败而用户侧无反馈）；`ListUserStarred` 路径构造改 `url.PathEscape` 兜底转义（历史脏值或其他写入路径漏校验时不再构成路径注入）；`syncStarsLocked` 对非法用户名跳过本轮、推进记账并 Warn 留痕 `star_sync_invalid_username`（与未配置同样避免每 1m 节拍空转）；补用户名字符集表驱动测试、含 `/` 用户名的线上转义路径断言、处理器 400 拒绝不覆盖已存合法值、轮询端跳过留痕四条回归 |
| 2026-09-23T00:00:00Z | 运行时配置解析与信封解密失败不再静默降级：`LoadStoredRuntime` 的 JSON 解析错误返回包装错误（`parse github runtime config`，与 `ai.LoadStoredConfig` 同语义）；`MergeFromStore` 中私钥/Webhook Secret 信封解密失败返回指明字段的包装错误（`decrypt github private key` / `decrypt github webhook secret`），不再以 `err == nil` 条件静默跳过——主密钥轮换后旧信封解不开时运行时会缺密钥，静默降级会让管理台显示「已配置」而根因不可见；补结构不符 JSON 与异密钥环信封两条回归测试 |
| 2026-09-20T00:00:00Z | 修正 `ListUserStarred` 翻页契约：Link 头是否有下一页须以 `rel="next"` 判断（末页仍带 `rel="prev"/"first"`、越界空页同样非空），不得以「头非空」判断；调用方 `syncx.StarredReleasePoller` 据此修复 unstar 移除被跳过的问题 |
| 2026-09-16T00:00:00Z | 新增 GetPRDiff（按 MaxPRDiffBytes+1 上限读取并透传读取错误）与 CreateIssueComment（App 身份 Bot 评论）；ListWorkflowJobs 改分页拉取（每页 100、最多 20 页，Link 头判断下一页）；自定义 HTTP 传输层优化连接池复用与请求超时 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

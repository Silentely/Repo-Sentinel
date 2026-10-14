# chatops

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **chatops**

## 模块职责

ChatOps 单兵运维交互子系统：集中管理带 TTL 与原子防重放的操作令牌（Action Token）、维护者白名单与 Actor 绑定校验、9 种复合动作执行（Issue/PR/Workflow）以及全平台就地状态回写反馈。

从原 `internal/httpapi` 中彻底剥离业务编排，使 HTTP 层仅负责传输适配，不再直接访问外部 API 或执行实体变更。

## 架构边界与接缝 (Seams)

- **入站接缝**：`chatops.Service`
  - `HandleTelegram(ctx, TelegramCallbackRequest) (*Outcome, error)`
  - `HandleFeishu(ctx, FeishuCallbackRequest) (*Outcome, error)`
  - `HandleSlack(ctx, SlackCallbackRequest) (*Outcome, error)`
  - `CreateToken(ctx, ActionData, ttl) (string, error)`
- **出站接缝**：
  - `ActionExecutor`：驱动 `githubx.AppClient` 与 `store.Store` 执行实体变更
  - `TelegramFeedback`：封装 Telegram Bot API `answerCallbackQuery` 与 `editMessageReplyMarkup`
  - `store.SettingsStore`：持久化令牌与幂等收据
- **下层叶子依赖**：`internal/sideeffect`（提供 `ExecuteWithReceipt` 幂等收据原语）

## 状态机与 Token 释放准则

- **StatusExecuted**：外部实体动作已完成，Token 保持 consumed。
- **StatusAlreadyDone**：此前已存在有效回执或已处理，幂等跳过，Token 保持 consumed。
- **StatusIndeterminate**：网络超时或远端在途不确定，**严禁释放 Token**，防止并发重试触发重复变更。
- **StatusRetryableFailure**：前置校验失败或远端明确 4xx 业务拒绝，使用 `context.WithoutCancel` 派生独立超时上下文释放 Token，允许安全重试。
- **StatusForbidden**：未通过维护者白名单（`chatops.<platform>.allowed_user_ids`）或 Actor 绑定不匹配。

## 测试与质量

- `token_test.go`：基于真实 SQLite 验证令牌防重放、并发 8 路抢占、上下文取消释放与过期失效。
- `security_test.go`：验证 Fail-Closed 安全配置解析、白名单逗号分隔匹配与 Actor 锁定。
- `service_test.go`：基于 Mock/Fake 验证三种平台完整生命周期与 Outcome 映射。

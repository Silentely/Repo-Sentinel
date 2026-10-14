# Domain Glossary

本文档定义 Repo-Sentinel 项目的领域统一设计词汇与概念边界。

## 1. ChatOps 运维交互体系

- **ChatOps Action (运维交互动作)**：面向单个代码仓库维护者的单兵快捷动作，通常以消息卡片内嵌按钮形式展现（如 `issue_mark_duplicate`、`issue_mark_spam_and_close`、`pr_quick_approve`、`workflow_rerun`）。
- **Action Token (操作令牌)**：带 TTL（默认 24 小时或 2 小时）、防重放、具有原子锁定与失败安全释放状态机的单次操作凭据。存储于 `system_setting` 中，键前缀为 `chatops_token:`。
- **Claim Token (认领凭据)**：用于在并发回调竞态下原子抢占令牌的互斥键，键前缀为 `chatops_claim:`。抢占成功的调用方获得执行权。
- **Action Receipt (副作用回执)**：针对外部写调用（如 GitHub API）的显式幂等审计收据（`side_effect:<type>:<repo>:<subject>:<rev>:<target>`），防止在途网络不确定性时重复触发远端副作用。
- **Platform Inbound Adapter (平台入站适配器)**：负责接收并规范化各 IM 平台（Telegram Webhook、飞书卡片交互、Slack Block Kit）的交互回调请求，提取操作者身份与 Token。
- **Platform Feedback Outbound (平台就地反馈)**：在动作执行完成或遇到错误时，向消息来源平台回写轻量反馈（如 Telegram `editMessageReplyMarkup` 键盘就地置灰折叠、飞书 Card Toast 提示、Slack Ephemeral 回复）。

## 2. 管道与状态机体系

- **Webhook Ingestion (Webhook 入口)**：验证 GitHub App 签名，快速入库并排入待处理通道。
- **Event Normalizer (事件规范化器)**：提取统一领域事件（`store.Event`），实现乱序容错与指纹幂等去重。
- **Rule Engine (实时规则引擎)**：根据仓库事件配置、静默时段与告警风暴抑制规则，评估通知意图并装配包含 ChatOps 操作按钮的通知载荷。
- **Notification Outbox (通知发件箱)**：保证通知持久化与断点恢复的核心队列表。
- **Outbox Worker (出站投递器)**：后台长轮询任务，负责按指数退避与安全网络策略将通知分发至全渠道平台。

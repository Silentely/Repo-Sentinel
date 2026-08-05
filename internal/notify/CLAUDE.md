# notify

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **notify**

## 模块职责

通知 Outbox 投递 Worker：周期 ClaimDue → 解密渠道密钥 → 发送 Telegram 或 HTTP Webhook → MarkSent / MarkRetry / MarkDead。

## 入口与启动

- `Worker.Run(ctx, interval)` — 默认 interval 5s；由 `app.Run` 启动
- 每 tick `ClaimDue(..., lock 2m, batch 20)`
- 退避阶梯 `defaultBackoff`（30s … 12h），`maxAttempts = 8`

常量：

- `AAD = "reposentinel:notify-secret:v1"` — 与写入端加密 AAD 必须一致

## 对外接口

无 HTTP；指标通过 `OnSent` / `OnDead` 回调（`httpapi.MetricsInc*`），避免反向依赖。

渠道类型：

- `telegram` — Bot API 富文本/可选 inline keyboard（HTMLURL）
- `http_webhook` — 出站 HTTPS + 可选 HMAC；`AllowPrivate` 控制私网

## 关键依赖与配置

- `store.Outbox` / `Channels`
- `cryptox.KeyRing` 解密 `SecretEnvelope`
- 安全 HTTP Client（超时、SSRF 相关限制见实现）

## 数据模型

消费/更新 `notification_outbox`；读取 `notification_channels`。

## 测试与质量

- `worker_test.go`、`worker_flow_test.go`、`telegram_test.go`、`http_webhook_test.go`、`retry_after_test.go`

## 常见问题 (FAQ)

**Q: 改 AAD 会怎样？**  
A: 历史渠道密钥无法解密，必须保持单一来源常量。

**Q: dead 如何复活？**  
A: 管理 API `POST .../outbox/{id}/retry` → `Outbox.RetryDead`。

## 相关文件清单

- `worker.go`（实现主体；测试文件同目录）

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

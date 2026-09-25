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
| 2026-09-25T09:30:00Z | 优化 Webhook 签名与投递错误码性能：sendHTTPWebhook 改用栈缓冲区生成 HMAC-SHA256 十六进制签名消除堆分配；状态码错误使用 strconv.Itoa 替代 fmt.Sprintf 消除装箱开销 |
| 2026-09-25T08:20:00Z | 优化通知管道文本截断与 HTML 转纯文本性能：消除 truncateLogTitle 的 []rune 堆分配；引入 isDeliveryCode 线性扫描替代正则匹配；htmlToPlainText 增加无标签与实体的零分配快速路径并支持大写 `<A HREF>`；truncateRunes 省略号前清理尾随空白；加固 Feishu/WeCom/DingTalk/Bark 机器人错误详情兜底回显 |
| 2026-09-25T07:25:00Z | 优化已删除渠道投递错误分类：当 `Channels().Get` 返回 `store.ErrNotFound` 时包装为 `channel_not_found` 并加入 `isPermanentDeliveryError`，立即标记死信并触发 `OnDead`，避免无谓重试 8 次耗时 30 小时 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

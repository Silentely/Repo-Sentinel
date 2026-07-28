# 能力与状态

> 版本见根目录 `VERSION`。下文描述当前代码中的能力与已知边界。

## 总览

| 判断 | 说明 |
|------|------|
| **可部署** | 推荐 `ghcr.io/silentely/repo-sentinel:latest` + Webhook + 对账/轮询 + 通知 + 管理后台 + 备份 CLI |
| **边界** | 不实现 GHES、多租户、PR Review 评论、个人通知收件箱 |

## 能力明细

| 能力 | 状态 | 备注 |
|------|------|------|
| 管理员 / Session / CSRF | 已交付 | |
| Webhook 双 Secret 验签、幂等 | 已交付 | |
| 事件规范化与指纹 | 已交付 | |
| 基线抑制通知 | 已交付 | 可手动「完成基线」或对账后自动 active |
| Installation Token 缓存 | 已交付 | 需配置 App ID + 私钥路径 |
| 自有仓 API 对账 | 已交付 | Scheduler 6h + 手动触发；页数预算 |
| 外部仓 Issues 轮询 | 已交付 | 默认 10 分钟；可选 PAT |
| 通知聚合 / 超频降级 | 已交付 | 默认 60s / 15 条 / 5 分钟；多实例靠 Outbox 时间桶幂等 |
| 每日摘要 | 已交付 | 默认本地 09:00 窗口；settings 可配 |
| Outbox 重试 / 死信 | 已交付 | |
| 仪表盘 / 列表 / GitHub / 关于 | 已交付 | 关于页可检查 GitHub Release |
| doctor / backup / restore | 已交付 | 备份须同时保管主密钥 |
| GHCR 镜像 | 已交付 | `main`/`dev` 推送对应浮动标签；`v*` 推送 `vX.Y.Z` + `latest` |
| Prometheus `/metrics` | 已交付 | 进程内计数 + 可选 Bearer；建议内网抓取 |

## 验证命令

```bash
go test ./...
pnpm --dir web typecheck
pnpm --dir web test -- --run
.tmp/reposentinel doctor
.tmp/reposentinel backup --output .tmp/backup.db
```

## 已知边界（有意为之）

1. 对账依赖 GitHub App 私钥与 Installation；未配置时对账接口返回不可用，Webhook 仍可用。  
2. 外部仓仅 Issues/PR（Issues API），不含 Actions/安全告警。  
3. 每日摘要按 settings 时区与本地时刻的小时窗口触发，非精确到秒的 cron。  
4. 通知聚合进程内合并为 best-effort；多副本靠 Outbox 幂等收敛，生产默认单实例更稳妥。  
5. `restore` 后必须用匹配主密钥启动，否则加密渠道凭据失效。  

# 能力与状态

> 对照实施计划 `docs/superpowers/plans/2026-07-28-production-mvp.md` 与设计规格。版本见根目录 `VERSION`。

## 总览

| 判断 | 说明 |
|------|------|
| **计划 9 切片** | 均已落地（见下表） |
| **可部署** | Docker Compose + Webhook + 对账/轮询 + 通知 + 管理后台 + 备份 CLI |
| **边界** | 不实现 GHES、多租户、PR Review 评论、个人通知收件箱 |

## 计划切片完成情况

| # | 切片 | 状态 | 主要位置 |
|---|------|------|----------|
| 1 | 领域 Schema + 迁移 | 完成 | `internal/store/ent/schema/*`、`migrations/*` |
| 2 | GitHub App 客户端 + Webhook | 完成 | `internal/githubx/*`、`httpapi/webhook_handlers.go` |
| 3 | Normalizer / 指纹 / 基线 / 乱序 | 完成 | `internal/normalizer/*` |
| 4 | Rule Engine + 聚合窗口 + Outbox | 完成 | `internal/rules/*` |
| 5 | Telegram / HTTP 投递 | 完成 | `internal/notify/worker.go` |
| 6 | Reconciler + 外部轮询 + Scheduler + 摘要 | 完成 | `internal/syncx/*`、`internal/digest/*` |
| 7 | 管理 API + 前端页面 | 完成 | `httpapi/monitor_handlers.go`、`web/src/features/monitor/*` |
| 8 | Docker + backup/restore/doctor + 文档 | 完成 | `Dockerfile`、`docker-compose.yml`、`internal/cli/*` |
| 9 | 测试与验收 | 完成 | `go test ./...`、前端 typecheck/vitest |

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
| 通知聚合 / 超频降级 | 已交付 | 默认 60s / 15 条 / 5 分钟 |
| 每日摘要 | 已交付 | 默认本地 09:00 窗口；settings 可配 |
| Outbox 重试 / 死信 | 已交付 | |
| 仪表盘 / 列表 / GitHub / 关于 | 已交付 | |
| doctor / backup / restore | 已交付 | 备份须同时保管主密钥 |
| GHCR 发布流水线 | 已交付 | `.github/workflows/docker.yml` → `ghcr.io/<owner>/repo-sentinel` |
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
2. 外部仓仅 Issues/PR（Issues API），不含 Actions/安全告警（规格非目标）。  
3. 每日摘要按 settings 时区与本地时刻的小时窗口触发，非精确 cron 到秒。  
4. 通知聚合在进程内存；多副本部署需粘性或后续外置（规格单实例）。  
5. `restore` 后必须用匹配主密钥启动，否则加密渠道凭据失效。  

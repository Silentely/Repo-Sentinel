# 能力与状态

> 更新日期以仓库提交为准。对照设计规格与当前代码，说明**已交付**与**持续增强**项，避免把路线图误认为线上能力。

## 总览

| 判断 | 说明 |
|------|------|
| **可部署闭环** | Webhook 验签入库 → 规范化 → 规则 → Outbox → Telegram/HTTP；管理仪表盘；Docker Compose |
| **设计规格全量** | 部分能力（深度对账、外部轮询、摘要调度、发布流水线等）仍可继续完善 |
| **文档** | VitePress 文档站（对齐 TG-SignPulse 导航结构） |

当前版本号见根目录 `VERSION`。

## 能力对照

| 领域 | 状态 |
|------|------|
| 配置、主密钥、双库迁移、管理员认证 | 已交付 |
| Webhook 验签、Inbox、事件规范化、乱序保护、基线抑制 | 已交付 |
| 自有仓基线与手动「完成基线」 | 已交付 |
| GitHub API 周期对账 / Installation Token 自动刷新 | 持续增强 |
| 外部公开仓登记 | 已交付 |
| 外部仓 Issues API 轮询与配额自适应 | 持续增强 |
| Rule Engine、Outbox、Telegram/HTTP | 已交付 |
| 通知时间窗聚合、每日摘要调度面板 | 持续增强 |
| 管理后台仪表盘与渠道配置 | 已交付 |
| 更完整的列表/筛选/系统页 | 持续增强 |
| Dockerfile + Compose | 已交付 |
| GHCR 多架构发布流水线 | 持续增强 |
| 原生 DB 备份约定 | 文档约定 |
| 应用内 backup/restore/doctor CLI | 持续增强 |

## 验证方式

```bash
go test ./...
pnpm --dir web test -- --run
pnpm --dir web typecheck
npm run docs:build
```

本地烟雾：启动服务 → `/health/ready` → 带签名 POST `/webhooks/github` → 同 Delivery 返回 duplicate。

## 建议迭代顺序

1. Installation Token 与自有仓增量对账  
2. 外部仓 Issues 轮询与限流  
3. 通知聚合与每日摘要  
4. GHCR 发布与应用内备份命令  

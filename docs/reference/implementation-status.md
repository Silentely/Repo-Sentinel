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
| 基线抑制通知 | 已交付 | 对账成功后自动 active；也可手动「立即放行」 |
| Installation Token 缓存 | 已交付 | 需配置 App ID + 私钥路径 |
| 自有仓 API 对账 | 已交付 | Scheduler 6h + 手动触发；页数预算 |
| 外部仓 Issues 轮询 | 已交付 | 默认 10 分钟；可选 PAT |
| Star / Watch 事件 | 已交付 | Webhook 实时采集；对账与外部轮询写 star 快照；受全局 / 仓库级 / 渠道订阅三层开关控制 |
| 通知聚合 / 超频降级 | 已交付 | 默认 60s / 15 条 / 5 分钟；多实例靠 Outbox 时间桶幂等 |
| 每日摘要 | 已交付 | 默认本地 09:00 窗口；settings 可配 |
| 周报 / 月报 | 已交付 | 可选（默认关闭）；发送日/发送时刻经 settings 配置，正文复用汇总模板 |
| 智能简报 / 告警分诊 | 已交付 | 可选（默认关闭）；OpenAI 兼容端点，可接本地模型；管理台可编辑（env 优先、密钥加密存库、热生效）；AI 不可用时自动降级为模板/原文 |
| Outbox 重试 / 死信 | 已交付 | |
| 仪表盘 / 列表 / GitHub / 关于 | 已交付 | 关于页可检查 GitHub Release；列表支持按仓筛选与本地忽略；仪表盘区块可折叠并记住状态 |
| 仪表盘 Star 增长曲线 | 已交付 | 全部仓库 Star 总数折线图，7/30/90/全部范围；无数据时显示引导文案 |
| 归档仓数据隔离 | 已交付 | 列表、侧栏计数、事件流与每日汇总默认排除已归档仓库；历史项仍保留在库中；GitHub 侧归档自动联动关闭采集 |
| 仓库删除清理 | 已交付 | GitHub 侧删除（repository.deleted）级联删除本地仓库与全部关联数据；对账 404/410、repositories_removed、transferred 标记不可用并暂停采集，可经 API 重新激活；仓库管理页提供「彻底删除」按钮兜底清理漏投递场景 |
| doctor / backup / restore | 已交付 | 备份须同时保管主密钥 |
| GHCR 镜像 | 已交付 | `main`/`dev` 推送对应浮动标签；`v*` 推送 `vX.Y.Z` + `latest` |
| Prometheus `/metrics` | 已交付 | 进程内计数 + 可选 Bearer；建议内网抓取 |
| 历史数据保留清理 | 已交付 | settings 可配事件/Outbox/Delivery 保留天数；后台每日清理；0 表示禁用该类 |

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
6. 周报/月报与每日摘要共用渠道的「接收定期汇总」开关，暂不支持按报告类型分渠道订阅。  
7. 智能值守功能默认关闭：未配置 `REPOSENTINEL_AI_API_KEY` 时不发起任何外部请求；AI 输出经 HTML 转义后嵌入通知，失败自动降级不影响投递。  

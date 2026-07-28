# 健康检查与版本

## 健康端点

| 路径 | 认证 | 含义 |
|------|------|------|
| `GET /health/live` | 无 | 进程存活 |
| `GET /health/ready` | 无 | 数据库与迁移等核心依赖就绪 |
| `GET /metrics` | 可选 Bearer | Prometheus 文本指标（见下方） |

示例：

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

容器或编排的 Healthcheck 应使用 `/health/ready`。

## Prometheus `/metrics` 是什么？

`/metrics` 是 **Prometheus 抓取格式**的指标 HTTP 接口（`text/plain`），给监控系统（Prometheus、VictoriaMetrics、Grafana Agent 等）定期拉取，用于做图表与告警。它**不是**给人看的管理 API。

当前暴露的计数/仪表包括（节选）：

- `reposentinel_webhook_accepted_total` / `duplicate_total` / `invalid_signature_total`
- `reposentinel_outbox_sent_total` / `outbox_dead_total`
- `reposentinel_reconcile_runs_total`
- 以及开放 Issue/PR、失败 Actions、安全告警、仓库数等 gauge

配置：

- `REPOSENTINEL_METRICS_ENABLED=true|false`（默认开启）
- `REPOSENTINEL_METRICS_TOKEN`：设置后抓取需带 `Authorization: Bearer <token>`

生产建议：反向代理只对内网开放 `/metrics`，或启用 Token。

## 版本 API

```http
GET /api/v1/system/version
```

需要有效管理员 Session。响应包含版本、Git SHA、分支、构建时间、构建渠道、Go 版本、数据库类型与 Schema 版本等字段（以实现为准）。

本地未注入 ldflags 时，CLI `version` 可能显示 `dev` / `unknown`，这是预期回退，不会被误判为正式发行版。

生产构建推荐：

```bash
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
.tmp/reposentinel version
```

`VERSION` 文件是 SemVer 真相源（当前 `0.1.0`）。正式 Tag 与镜像规则见设计规格；**GHCR 发布流水线尚未落地**。

## 更新检查

规格中的「关于页主动检查 GitHub Release」**尚未实现**。请关注仓库 Release 或自行比对 `VERSION`。

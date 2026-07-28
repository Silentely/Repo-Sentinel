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

`VERSION` 文件是 SemVer 真相源（当前 `0.3.3`）。正式 Tag（`v*`）由 [`.github/workflows/docker.yml`](https://github.com/Silentely/Repo-Sentinel/blob/main/.github/workflows/docker.yml) 构建并推送 GHCR，包含 `latest` / `main` 等标签；`main` 分支推送只跑测试不构建镜像；`dev` 推送预发 `dev` 标签。部署请使用 `ghcr.io/silentely/repo-sentinel:latest`（或钉死的 `v*`），无需本地构建。

## 更新检查

管理后台「关于与版本」提供 **检查更新**：优先通过 `github.com/.../releases/latest` 的 302 Location 解析 tag（不占用 API 配额），失败再回退 API JSON；失败 soft-fail；成功结果进程内缓存约 6 小时。

| 配置 | 说明 |
|------|------|
| `REPOSENTINEL_UPDATE_CHECK` | 默认开启；`false`/`0`/`off` 关闭远程检查 |
| `REPOSENTINEL_UPDATE_CHECK_URL` | 默认 GitHub API `releases/latest`；可换自定义 **https** JSON 源 |
| `REPOSENTINEL_UPDATE_CHECK_TOKEN` | 可选，仅 JSON/API 路径使用 |

```http
GET  /api/v1/system/version
POST /api/v1/system/version/check?force=true
```

均需管理员 Session；`POST` 另需 CSRF。响应含 `update_check`（`latest_version` / `update_available` / `error` / `cached` 等）。

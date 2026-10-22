# 健康检查与版本

## 健康端点

| 路径 | 认证 | 含义 |
|------|------|------|
| `GET /health/live` | 无 | 进程存活 |
| `GET /health/ready` | 无 | 数据库与迁移等核心依赖就绪 |
| `GET /metrics` | 可选 Bearer | Prometheus 文本指标（见下方） |
| `GET /api/v1/system/health` | 管理员 Session | 综合系统健康诊断与子系统指标（数据库、存储体积、Outbox 队列、GitHub API 限流配额与 AI 预算状态；DB 故障返回 503，外部依赖/AI熔断时降级返回 200 + `status: "degraded"`） |

示例：

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

容器或编排平台的健康检查应使用 `/health/ready`。

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


### 综合系统健康诊断 API (`/api/v1/system/health`)

面向管理平台与运维看板的深度诊断端点（需管理员 Session 认证）：

```http
GET /api/v1/system/health
```

**响应结构示例：**
```json
{
  "database_ok": true,
  "database_driver": "sqlite",
  "goroutines": 32,
  "memory_alloc_mb": 45,
  "memory_sys_mb": 112,
  "uptime_seconds": 3600,
  "status": "ok",
  "storage": {
    "driver": "sqlite",
    "file_size_bytes": 10485760,
    "wal_size_bytes": 2097152
  },
  "outbox": {
    "pending_count": 0,
    "delivered_count": 128,
    "dead_count": 0
  },
  "github": {
    "configured": true,
    "rate_limit_limit": 5000,
    "rate_limit_remaining": 4820,
    "status": "ok"
  },
  "ai_budget": {
    "daily_tokens_used": 15400,
    "daily_token_limit": 100000,
    "daily_calls_used": 12,
    "daily_call_limit": 200,
    "is_throttled": false
  }
}
```

**故障降级语义：**
- **强依赖故障**：底层数据库连接失败时，坚决返回 **HTTP 503 Service Unavailable**，保障集群探针判死语义；
- **弱依赖与软限制**：仅当外部依赖探测超时（如 GitHub API 配额探测）或触发当日 AI 预算熔断时，返回 **HTTP 200 OK** 并标明 `"status": "degraded"`，系统进入局部平稳降级模式。

## GitHub 配额异常如何排查

健康探针先用 App JWT 查询安装列表，再用首个未暂停安装的 installation access token 请求 `/rate_limit`。App JWT 用于 App 身份认证，不能直接作为安装凭据查询该配额。

仪表盘显示的是**一个可用安装的 REST core 配额**，不是所有安装的总额，也不是 GraphQL 配额。各安装配额独立；探针正常也不能证明每个仓库的权限齐全。探针结果缓存 60 秒。

| 状态或错误 | 含义与处理 |
|------|------|
| `未知（探针失败）` | 配额未能读取。API 响应中的默认 `0 / 0` 不表示配额耗尽，应同时查看 `github.status` 和 `github.error` |
| `github_app_no_active_installation` | App 尚未安装，或所有安装均暂停；先安装或恢复安装 |
| `github_probe_installations: github_http_401` | 获取安装列表时鉴权失败；核对 App ID、对应 RSA 私钥与服务器时间 |
| `github_probe_token: installation_token_http_401` | 签发安装 token 时鉴权失败；核对 App 凭据与服务器时间 |
| 错误包含 `http_403` | 核对安装状态、仓库范围和权限批准；若错误为 `github_rate_limited`，按限流处理 |
| `github_probe_rate_limit: ...` | 已进入安装配额查询阶段，按后续 HTTP 错误或网络错误排查 |
| 剩余配额为 `0`，上限大于 `0`，且无探针错误 | 已成功读到配额，当前安装的 REST 配额确实耗尽，等待重置 |
| `graphql_permission_denied` / `Resource not accessible by integration` | PR 补充查询缺少权限或仓库访问授权；与 REST 配额探针分别排查 |

权限清单、新增权限批准与 token 刷新步骤见 [GitHub App 仓库权限配置](/deploy/docker#_4-5-repository-permissions-按功能授权)。Webhook 使用独立的 Secret 验签，Webhook 接收正常与 API 调用失败可以同时发生。

## 版本 API

```http
GET /api/v1/system/version
```

需要有效管理员 Session。响应包含版本、Git SHA、分支、构建时间、构建渠道、Go 版本、数据库类型与 Schema 版本等字段（以实现为准）。

### 公开构建信息

```http
GET /api/v1/system/build-info
```

**无需认证**，仅返回 `{ "version": "x.y.z" }` 一个字段（示例值，以实例实际版本为准）。供登录页页脚等未认证场景展示真实构建版本；不含任何配置状态（需要完整版本信息请用上一条 `system/version`）。

本地构建未通过构建参数（ldflags）注入版本信息时，CLI `version` 可能显示 `dev` / `unknown`，这是预期回退，不会被误判为正式发行版。

生产构建推荐：

```bash
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
.tmp/reposentinel version
```

部署镜像推荐：`ghcr.io/silentely/repo-sentinel:latest`（或固定为 `vX.Y.Z`），见 [Docker 部署](/deploy/docker)。  
产品版本以仓库根目录 `VERSION` 为准；维护者发版见 [发布与镜像](/reference/release)。

## 更新检查

管理后台「关于与版本」提供 **检查更新**：优先通过 `github.com/.../releases/latest` 的 302 Location 解析 tag，不占用 API 配额。解析失败再回退到 API JSON。所有远程检查失败均优雅降级，不影响服务运行；成功结果进程内缓存约 6 小时。

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

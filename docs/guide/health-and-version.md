# 健康检查与版本

## 健康端点

| 路径 | 认证 | 含义 |
|------|------|------|
| `GET /health/live` | 无 | 进程存活 |
| `GET /health/ready` | 无 | 数据库与迁移等核心依赖就绪 |

示例：

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
```

容器或编排的 Healthcheck 应使用 `/health/ready`（完整 Docker 镜像发布仍属后续阶段）。

::: tip
规格中的 `/health/detail`（管理员详细诊断）与 `/metrics` **尚未实现**。
:::

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

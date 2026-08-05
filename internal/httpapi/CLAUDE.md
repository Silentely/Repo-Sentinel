# httpapi

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **httpapi**

## 模块职责

HTTP 管理面：Chi 路由、认证/CSRF 中间件、JSON 错误约定、Prometheus metrics、SPA 回退，以及管理 REST API。Webhook 业务编排委托 `webhooksvc`。

## 入口与启动

- `httpapi.New(Dependencies) http.Handler` — 由 `app.Build` 调用
- Cookie 名：`reposentinel_session` / `reposentinel_csrf`；写请求头 `X-CSRF-Token`

全局中间件顺序：requestID → realIP → accessLog → recovery → securityHeaders。

## 对外接口

### 公开

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health/live` | 存活 |
| GET | `/health/ready` | 就绪 |
| GET | `/metrics` | Prometheus（可关/可 Bearer） |
| POST | `/webhooks/github` | GitHub Webhook（`githubx.WebhookPath`） |
| GET | `/api/v1/setup/status` | 是否需要首次设置 |
| POST | `/api/v1/setup` | 创建管理员 |
| POST | `/api/v1/auth/login` | 登录 |

### 需 Session

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/api/v1/auth/session` | 当前会话 |
| GET | `/api/v1/system/version` | 版本信息 |
| GET | `/api/v1/dashboard` | KPI |
| GET | `/api/v1/repositories` | 仓库列表 |
| GET | `/api/v1/work-items` | Issue/PR |
| GET | `/api/v1/workflow-runs` | Actions |
| GET | `/api/v1/security-alerts` | 安全告警 |
| GET | `/api/v1/events` | 事件 |
| GET | `/api/v1/notifications/outbox` | 发件箱 |
| GET | `/api/v1/notifications/channels` | 渠道 |
| GET | `/api/v1/github/installations` | 安装列表 |
| GET | `/api/v1/github/config` | GitHub 配置（掩码） |
| GET | `/api/v1/ai/config` | AI 配置（掩码） |
| GET | `/api/v1/system/settings` | 系统设置 |

### 需 Session + CSRF（写）

含 logout、改密、渠道 CRUD/测试/开关、Outbox 重试、仓库激活/对账/设置、忽略标记、全量对账、设置/GitHub/AI 配置写入、同步安装仓、版本检查等（完整列表见 `server.go`）。

未匹配路径：SPA handler（嵌入前端）或 API 404。

## 关键依赖与配置

`Dependencies`：Config、Store、Admin/Session 服务、CSRF、LoginLimiter、BuildInfo、Ready、Logger、Frontend FS、KeyRing、Aggregator、Reconciler、UpdateChecker、GitHubRuntime、AI、AIRuntime、Background context。

## 数据模型

无自有表；JSON 响应映射 `store` 领域模型。

## 测试与质量

- 多组 `*_handlers_test.go`、`middleware_test.go`、`spa_test.go`、`metrics_test.go`
- 后台任务用 `safeGo` 防 panic 拖垮进程

## 常见问题 (FAQ)

**Q: Webhook 同步还是异步？**  
A: HTTP 层验签入库后，规范化/通知在 Background 上异步执行（`webhooksvc.Process`）。

**Q: 全量对账会并发吗？**  
A: `reconcileAllRunning` atomic 防重入。

## 相关文件清单

- `server.go` — 路由注册
- `*_handlers.go` — 各域 handler
- `middleware.go`、`security_headers.go`、`spa.go`、`json.go`、`errors.go`、`metrics.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

# httpapi

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **httpapi**

## 模块职责

HTTP 管理面：Chi 路由、认证/CSRF 中间件、JSON 错误约定、Prometheus metrics、SPA 回退、管理 REST API，以及面向 AI Agent 的发现端点（sitemap/robots/Content-Signals、RFC 8288 Link 头、RFC 9727 API 目录、OpenAPI、OAuth 2.0 client-credentials、RFC 9728 受保护资源、auth.md、MCP Streamable HTTP、Agent Skills 索引、Markdown 协商）。Webhook 业务编排委托 `webhooksvc`。

## 入口与启动

- `httpapi.New(Dependencies) http.Handler` — 由 `app.Build` 调用
- Cookie 名：`reposentinel_session` / `reposentinel_csrf`；写请求头 `X-CSRF-Token`
- Agent 访问令牌：`Authorization: Bearer <JWT>`（OAuth client_credentials 签发）

全局中间件顺序：requestID → realIP → accessLog → recovery → securityHeaders → agentLinkHeaders。
SPA 兜底外层包 Markdown 协商中间件（`Accept: text/markdown` 返回站点 markdown 说明）。

## 对外接口

### 公开

| 方法 | 路径 | 说明 |
|------|------|------|
| GET | `/health/live` | 存活 |
| GET | `/health/ready` | 就绪 |
| GET | `/metrics` | Prometheus（可关/可 Bearer） |
| GET | `/robots.txt` | 站点规则 + Sitemap + Content-Signals |
| GET | `/sitemap.xml` | 管理台规范路由（动态生成） |
| GET | `/auth.md` | Agent 认证与注册说明 |
| GET | `/openapi.json` | OpenAPI 3.1 接口描述 |
| GET | `/.well-known/api-catalog` | RFC 9727 API 目录（linkset+json） |
| GET | `/.well-known/oauth-authorization-server` | RFC 8414 授权服务器元数据 |
| GET | `/.well-known/oauth-protected-resource` | RFC 9728 受保护资源元数据 |
| GET | `/.well-known/agent-skills/index.json` | Agent 技能发现索引 |
| GET | `/.well-known/agent-skills/reposentinel-api/SKILL.md` | 技能工件 |
| GET | `/.well-known/mcp/server-card.json` | MCP Server Card（SEP-1649） |
| GET | `/oauth/jwks` | 令牌签名公钥（JWKS） |
| GET/POST | `/oauth/authorize` | 声明端点（不支持交互式授权流，返回 400） |
| POST | `/oauth/token` | OAuth 2.0 client_credentials 令牌签发 |
| POST | `/webhooks/github` | GitHub Webhook（`githubx.WebhookPath`） |
| GET | `/api/v1/setup/status` | 是否需要首次设置 |
| POST | `/api/v1/setup` | 创建管理员 |
| POST | `/api/v1/auth/login` | 登录 |

### 需 Session 或 Bearer

| 方法 | 路径 | 说明 |
|------|------|------|
| POST | `/mcp` | MCP Streamable HTTP 网关（JSON-RPC 2.0） |
| GET | `/api/v1/auth/session` | 当前会话（仅 Session） |
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

含 logout、改密、渠道 CRUD/测试/开关、Outbox 重试、仓库激活/对账/设置、忽略标记、全量对账、设置/GitHub/AI 配置写入、同步安装仓、版本检查等（完整列表见 `server.go`）。Bearer 认证（Agent）跳过双提交 CSRF 校验。

未匹配路径：SPA handler（嵌入前端）或 API 404。

## 关键依赖与配置

`Dependencies`：Config、Store、Admin/Session 服务、CSRF、LoginLimiter、BuildInfo、Ready、Logger、Frontend FS、KeyRing、Aggregator、Reconciler、UpdateChecker、GitHubRuntime、AI、AIRuntime、Background context。Agent 发现端点另依赖 `BuildInfo.Version`（OpenAPI / MCP Card）与 `KeyRing`（OAuth 令牌签名，`DeriveHMACKey` 派生 HS256 密钥）。

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
- `agent_discovery.go` — sitemap/robots/auth.md/OpenAPI/well-known/MCP Card/Skills 索引与 Markdown 协商
- `oauth.go` — OAuth 2.0 client-credentials 令牌端点、JWKS、Bearer 校验
- `mcp.go` — MCP Streamable HTTP 网关（JSON-RPC 2.0 + 只读工具）
- `*_handlers.go` — 各域 handler
- `middleware.go`、`security_headers.go`、`spa.go`、`json.go`、`errors.go`、`metrics.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-10T13:00:00Z | 新增 starred-releases 端点：GET/PUT `/api/v1/starred-releases/config`（用户名归一化、周期/上限/预发布开关）、POST `/api/v1/starred-releases/sync`（立即同步）、GET `/api/v1/starred-releases/trackers`（分页 + state 筛选）、POST `/api/v1/starred-releases/trackers/{id}/state`（停用/恢复）；AI 配置 API 增 `release_summary_enabled` |
| 2026-08-06T15:48:41Z | 新增 /api/v1/stats/star-trend 端点（days=7/30/90/0），/api/v1/repositories/{id}/settings 支持 stars_enabled、watches_enabled 字段 |
| 2026-08-06T12:50:00Z | Agent 发现端点：sitemap/robots/Content-Signals、Link 头、RFC 9727 API 目录、OpenAPI 3.1、OAuth 2.0 client-credentials（token/jwks/Bearer 认证）、RFC 8414/9728 元数据、auth.md、MCP Streamable HTTP 网关 + Server Card、Agent Skills 索引、Markdown 协商；`authenticationMiddleware` 支持 Bearer，`csrfMiddleware` 对 Agent 放行 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

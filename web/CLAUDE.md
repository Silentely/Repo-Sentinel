# web

[根目录](../CLAUDE.md) > **web**

## 模块职责

管理控制台前端（React 19 + TypeScript）及 Go embed 适配：开发态 fallback、生产态嵌入 `dist`，由后端同一端口提供 SPA。

## 入口与启动

| 入口 | 说明 |
|------|------|
| `src/main.tsx` | React 挂载 |
| `src/app/router.tsx` | TanStack Router 路由树 |
| `src/app/providers.tsx` | QueryClient 等 Provider |
| `src/app/root-layout.tsx` | 侧栏壳层、健康胶囊、主题 |
| `files.go` + `embed_*.go` | 供 `app.Build` 加载前端 FS |

开发：

```bash
pnpm --dir web install
pnpm --dir web dev          # Vite
pnpm --dir web typecheck
pnpm --dir web test -- --run
pnpm --dir web build        # 产出 dist，供 production embed
```

packageManager：`pnpm@10.34.5`；Node `>=24`。

## 对外接口

### 前端路由

| 路径 | 页面 |
|------|------|
| `/login` | 登录 |
| `/setup` | 首次创建管理员 |
| `/` | 仪表盘 |
| `/notifications` | 渠道配置 |
| `/notifications/outbox` | 发件箱 |
| `/issues` | Issue 列表 |
| `/pull-requests` | PR 列表 |
| `/repos` | 仓库 |
| `/actions` | Actions |
| `/security` | 安全告警 |
| `/github` | GitHub 配置/安装 |
| `/about` | 关于与版本检查 |

认证路由：`_authenticated` 布局；仅 HTTP 401 跳转登录，网络错误保留会话提示重试。

### 调用的后端 API

集中在 `features/auth/api.ts` 与 `features/monitor/api.ts`（`apiRequest` → `/api/v1/...`）。

## 关键依赖与配置

- React 19、TanStack Router / Query、react-hook-form、zod、lucide-react
- 构建：Vite 8、`@tailwindcss/vite`（工具链）；产品样式以 `styles/tokens.css` + `globals.css` 为主
- 测试：Vitest + Testing Library；E2E：Playwright（`e2e/`）
- 代理/基址：见 `vite.config.ts`（开发时转发 API）

## 数据模型

TypeScript 类型与后端 JSON 对齐，定义于：

- `features/monitor/api.ts` — Dashboard、Repository、Event、Outbox、Channel 等
- `features/auth/schemas.ts` — 登录/设置表单

## 测试与质量

- 单元：`*.test.ts(x)`（auth、components、monitor 等）
- E2E：`e2e/auth.spec.ts`、`mobile-nav.spec.ts`
- 质量门禁：`pnpm typecheck` 必须通过（`make test-frontend`）

## 常见问题 (FAQ)

**Q: 路由是否在 `src/routes/`？**  
A: 否。路由树在 `src/app/router.tsx`；页面在 `features/*`。

**Q: 生产如何嵌入？**  
A: `make build-production` 使用 `-tags production`，`embed_production.go` 嵌入已构建的 `dist`。

**Q: 忽略 `web/dist` 与 `node_modules`？**  
A: AI/文档扫描应忽略；构建产物由 CI/Docker 多阶段生成。

## 相关文件清单

- `src/app/*`、`src/features/auth/*`、`src/features/monitor/*`
- `src/components/*`、`src/lib/api/*`、`src/styles/*`
- `package.json`、`vite.config.ts`、`vitest.config.ts`、`playwright.config.ts`
- `embed_dev.go`、`embed_production.go`、`files.go`、`fallback/index.html`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

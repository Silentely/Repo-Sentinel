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
| 2026-09-25T09:45:00Z | 优化仓库管理页渲染性能：使用 useMemo 单次线性遍历切分活跃与归档仓库，消除多轮重复 filter；使用 React.memo 包裹 RepoCard，避免开关切换与翻页时未修改仓库的冗余重渲染 |
| 2026-09-25T08:00:00Z | 监控页面渲染性能优化：①`StarredReleasesPage` 记忆化 `items` 数组与 `handleToggle` 回调，并将 `TrackerRow` 包裹 `memo`，消除状态变更或轮询时全量追踪列表重渲染；②`OutboxPage` 记忆化 `items` 列表与 `deadCount` 统计，避免每次渲染重复执行全量数组过滤；③`WebhookDeliveriesPage` 记忆化 `items` 列表引用，保持列表数据引用稳定性 |
| 2026-09-25T07:48:00Z | 投递文本与渠道渲染优化：①`htmlToPlainText` 增强对单双引号属性、大小写标签、额外属性（如 `target="_blank"`）与标准库数字实体（`&#34;`、`&#39;`、`&apos;`）的完整反转义支持；②`outboxErrorHint` 补全 `channel_not_found`（渠道被删除）与 `missing_target` 错误码中文排障提示；③`NotifyPage` 增加渠道列表引用与渠道类型索引 Map 记忆化（useMemo），消除每次渲染重复创建数组和多轮线性查找开销 |
| 2026-09-25T07:32:00Z | 交互与时间格式健壮性优化：①`useModalLayer` 修复 Hooks cleanup 访问 ref.current 的 oxlint 告警，达到 0 warning 0 error；②`repositoriesQueryOptions` 增加跨页 ID 去重防 React key 冲突；③`parseGoDurationSeconds` 移除非 Go 标准库的 `d` 单位，与 Go `time.ParseDuration` 严格对齐并消除 400 校验错误 |
| 2026-09-23T00:00:00Z | Star Release 页 `setPage` 改 `useCallback` 稳定引用：分页超界钳制的 effect 依赖它，内联箭头每次渲染都是新函数、会让该 effect 每渲染重跑（oxlint `react-hooks/exhaustive-deps` 告警），稳定后告警消除 |
| 2026-09-23T00:00:00Z | 接入 oxlint 静态检查（`pnpm lint`，配置 `.oxlintrc.json`，correctness 类别为 error）：项目使用 typescript 7.0.2，超出 typescript-eslint 的 peer 支持范围（`<6.1.0`，parser 直接抛 `typescript-eslint does not support TS 7.0`），故采用不依赖 TypeScript 版本的 oxlint，类型正确性仍由 `tsc --noEmit` 保证；`react/set-state-in-effect`、`react/preserve-manual-memoization` 两条 React Compiler 风格规则关闭（notify-page 回填、number-field 外部值同步均为「外部状态同步到本地 state」的正当 effect 场景）；据 lint 结果清理 7 处未使用导入（about-page/github-page/dashboard-page/outbox-page/webhook-deliveries-page/settings-page/webmcp.test），仪表盘与设置页 `repoItems` 源数组入 memo（`repos.data?.items ?? []` 每次渲染产出新引用，此前下游 useMemo 依赖永不相等、派生缓存实际无效），审查结论三类清单（安全风险/破坏性兼容风险/优化建议）抽取为 `ReviewRiskList` 组件并以 `${idx}-${item}` 作 key（条目文本可能重复，纯文本 key 会撞键）；主题选择与登录页剩余重试次数改用收敛函数取代 `as ThemeMode`、`as Record<string, unknown>` 强转；补审查结论清单回归一条 |
| 2026-09-23T00:00:00Z | GitHub 配置表单回填补「仅首次」守卫（`hydratedRef`，与设置页、Star Release 页同一模式）：保存、同步仓库等 mutation 触发 invalidate 后配置 refetch 得到新对象引用，旧实现会再次整体回填、把用户在 App ID / Client ID / Public Base URL 输入框中的未保存编辑静默覆盖为服务端值；Webhook 投递历史页：载荷查询失败不再落到「未找到记录详情」（那会把服务端或网络错误误报为记录不存在，改渲染 `ApiErrorAlert`），页码同步到 URL（`?page=`，刷新或复制链接后停留在原页，含 0/非数字回退第 1 页）；补 GitHub 页 refetch 不回填与 Webhook 页载荷错误分支、翻页写 URL、非法页码回退四条回归（均先还原实现验证可捕获缺陷） |
| 2026-09-23T00:00:00Z | 2FA 状态查询失败不再回落到「未开启」徽章与配置引导（改为「状态未知」+ `ApiErrorAlert` 错误条，此前网络失败会被误读为账号未受保护而重复配置），`enable2FA` 的 `setupData!.secret` 非空断言改为显式守卫并抛出带 `two_factor_setup_missing` 错误码的 `ApiError`；Webhook 投递历史页 Inspector 模态改接 `useModalLayer`（原手写 Escape 监听缺背景滚动锁、焦点循环与焦点归还），删除随之无引用的 `useEffect` 导入；补 2FA 卡片三条回归（状态未知渲染、开启成功后状态失效并收起密钥区块、校验失败透出服务端文案，均先还原实现验证可捕获缺陷） |
| 2026-09-23T00:00:00Z | AIReviewCard 生命周期与渲染收口：审查轮询循环在组件卸载后立即停止并跳过回调（卸载不会取消进行中的 async 函数，此前会按 2s 节奏拉取审查结果到 90s 上限、对已卸载组件写状态）；「已复制」提示的复位定时器改为引用持有，卸载或再次复制即清除（此前每次复制新起悬挂定时器，卸载后仍写组件状态）；卡片包 `memo`（列表切换忽略忙碌态/筛选/分页时 props 未变的审查卡片不再连带重渲染）并按 `TrackerRow` 先例导出以便直接单测；补卸载即停轮询、卸载清定时器、审查落定自动展开三条回归 |
| 2026-09-23T00:00:00Z | Star Release 页表单回填补「仅首次」守卫（`hydratedRef`，与设置页同一模式）：配置被窗口重新可见等触发 refetch 时不再整体回填表单、覆盖未保存编辑（此前在用户名输入中途切走再回来，编辑被静默重置为服务端值）；「立即同步」轮询循环在组件卸载后立即停止并跳过回调（mutation 生命周期不随组件卸载取消，此前卸载后仍按 2s 节奏轮询到 90s 上限、继续拉取配置并对已卸载组件执行查询失效）；补卸载即停与 refetch 不回填两条回归（均先还原实现验证可捕获缺陷） |
| 2026-09-20T00:00:00Z | Star Release 页「立即同步」改等待同步落定：POST 返回仅代表同步启动（后端异步执行），轮询配置 `last_star_sync_at` 推进后再失效并刷新追踪列表，落定成功/未配置/超过等待窗口三种结论分别提示（原实现在异步同步完成前刷新，用户看到同步前状态以为未生效） |
| 2026-09-16T00:00:00Z | 新增 AIReviewCard 审查报告卡片（健康评分彩色标签、风险等级徽章、Bot 作者标记、手动触发/重新审查、一键复制 Markdown、加载失败与 Diff 截断提示）与安全警示横幅；新增 Webhook 投递历史管理页（状态/事件/仓库筛选、载荷 Inspector、一键重放）；设置页新增审查开关与评论回写选项；手动触发审查改轮询 head_sha 与 reviewed_at；监控模块查询失效改 Promise.all 并行执行；RelativeTime 全局单一定时器订阅分发并每 60 秒刷新 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

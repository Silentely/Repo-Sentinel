# Phase 4: 前端体验与运维观测 Implementation Plan (Hardened & Grok/Codex Approved)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 打造前端通知效果模拟器、基于既有 SSE 基础设施的 Webhook 实时交付瀑布流看板，并扩充系统健康与诊断指标接口，实现具备背压控制与部分降级保护的高效运维观测体系。

**Architecture:**
1. 复用仓库既有的 `SSEHub`（`internal/httpapi/sse_hub.go`、`/api/v1/events/stream`、`web/src/lib/sse-client.ts`），通过扩展广播主题挂载 Webhook 处理五阶段瀑布流事件，避免重复构造 SSE 轮子；在现有的 `webhook-deliveries-page.tsx` 中绘制实时交付生命周期瀑布图；
2. 构建基于后端共享试算接口（`/api/v1/rules/dry-run`）的通知模拟器抽屉，支持近期事件与 4 套标准 Mock 事件，实时预览多渠道渲染并精准解释规则拦截原因；
3. 在 `internal/store/domain.go` 中建立统一的 `DiagnosticStore` 接口，扩充 `internal/httpapi/system_handlers.go` 中的健康检查端点，支持 DB 异常时维持 503、仅外部探针故障时返回 200 Partial Degraded。

**Tech Stack:** Go 1.24+, React 19, TypeScript, Tailwind CSS, Vite, Vitest, SSEHub

**Spec:** `docs/superpowers/specs/2026-10-05-global-architecture-evolution-design.md`  
**Review Baselines:** `docs/superpowers/plans/2026-10-05-codex-plan-review.md`, `docs/superpowers/plans/2026-10-05-grok-plan-review.md`

## Global Constraints
- 前端测试与构建双全绿：TypeScript 代码必须严格符合类型系统，运行 `npm run build` 和 `npm test` 必须全部无报错无警告通过。
- 资产复用与路径一致性：坚决杜绝创建平行的前端目录或重复的 SSE 设施，所有改动严格挂载在 `web/src/features/monitor/` 与既有 `SSEHub` 体系下。
- 诊断接口容错边界：`/api/v1/system/health` 遇到 DB 故障必须坚持返回 HTTP 503（保证 readiness 探针语义正确），仅当遇到外部组件（如 GitHub API 配额探测）短暂故障时，方返回 HTTP 200 及局部 `status: "degraded"`。

---

### Task 1: 基于既有 SSEHub 的 Webhook 实时交付瀑布流 (Live Inspector)

**Files:**
- Modify: `internal/httpapi/sse_hub.go`
- Modify: `internal/httpapi/server.go:290-310`
- Modify: `internal/webhooksvc/service.go:30-50`
- Modify: `web/src/features/monitor/webhook-deliveries-page.tsx`
- Create: `web/src/features/monitor/components/live-inspector-waterfall.tsx`
- Test: `internal/httpapi/sse_hub_test.go`

**Interfaces:**
- Consumes: `SSEHub.Broadcast`, `web/src/lib/sse-client.ts`
- Produces: Webhook 五阶段事件流广播、`<LiveInspectorWaterfall />`

- [x] **Step 1: 编写基于既有 SSEHub 的五阶段事件广播与前端客户端测试**

扩展 `internal/httpapi/sse_hub_test.go` 与前端 `web/src/lib/sse-client.test.ts`：
- 模拟 Webhook 完整处理，验证经由 `OnBroadcast` 广播的五阶段事件：`accepted`、`processing`、`rules_evaluated`、`outbox_queued`、`channel_delivered` 能够被客户端按序接收；
- 验证既有的鉴权拦截（未授权返回 401）、15 秒心跳保活与慢消费者缓冲区满自动清理机制。

- [x] **Step 2: 运行测试验证失败**

Run: `go test -v -run TestSSEHub ./internal/httpapi/...`
Expected: FAIL

- [x] **Step 3: 扩展事件广播与前端瀑布流组件**

1. 在 `internal/webhooksvc/service.go` 中，在每个关键处理阶段调用 `s.broadcastEvent("delivery.stage", stagePayload)`；
2. 在 `web/src/features/monitor/components/live-inspector-waterfall.tsx` 中编写瀑布图，以彩色横道展示各阶段毫秒耗时与状态结果；
3. 在现有的 `web/src/features/monitor/webhook-deliveries-page.tsx` 页面顶部嵌入实时监测卡片，支持开启/暂停自动滚动。

- [x] **Step 4: 运行测试验证通过**

Run: `go test -v -race -run TestSSEHub ./internal/httpapi/...`
Expected: PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add internal/httpapi/ internal/webhooksvc/ web/src/features/monitor/
git commit -m "feat(monitor): introduce live inspector waterfall on top of existing SSEHub"
```

---

### Task 2: 通知效果多渠道模拟器 (Notification Simulator)

**Files:**
- Create: `internal/httpapi/simulator_handlers.go`
- Create: `web/src/features/monitor/components/notification-simulator-drawer.tsx`
- Create: `web/src/features/monitor/components/channel-preview-cards.tsx`
- Modify: `web/src/features/monitor/notify-page.tsx`
- Test: `web/src/features/monitor/components/simulator.test.tsx`
- Test: `internal/httpapi/simulator_test.go`

**Interfaces:**
- Consumes: `POST /api/v1/rules/dry-run` (后端统一规则试算)
- Produces: `<NotificationSimulatorDrawer />`

- [x] **Step 1: 编写规则试算后端接口与前端模拟器组件测试**

创建 `internal/httpapi/simulator_test.go` 与 `web/src/features/monitor/components/simulator.test.tsx`：
- 后端测试：调用 `/api/v1/rules/dry-run`，传入 Mock 事件与现有渠道规则，返回各渠道是否匹配及拦截原因；
- 前端测试：选择“Issue Opened”事件时，飞书与 Slack 预览能够正确渲染标题、标签和操作按钮；当事件命中被忽略机器人或分支规则时，高亮显示拦截原因。

- [x] **Step 2: 编写抽屉与多渠道卡片预览组件**

1. 在 `channel-preview-cards.tsx` 中实现 4 种渠道格式的真实视觉渲染（飞书交互卡片、Telegram MarkdownV2、Slack Block Kit、企业微信）；
2. 在 `notification-simulator-drawer.tsx` 中封装右侧滑出抽屉，提供“预置模版”与“从近 5 次历史事件中选取”的切换，并通过后端 `/api/v1/rules/dry-run` 获取权威的拦截诊断结果。

- [x] **Step 3: 集成至通知配置主页并验证前端单元测试与编译**

修改 `web/src/features/monitor/notify-page.tsx` 添加“模拟测试”快捷入口，运行：
```bash
cd web && npm test && npm run build
```
预期结果：Vitest 单元测试全过，构建成功零错误零警告。

- [x] **Step 4: 提交本 Task 改动**

```bash
git add internal/httpapi/ web/src/features/monitor/
git commit -m "feat(ui): add multi-channel notification simulator drawer with shared backend rule dry-run"
```

---

### Task 3: 系统诊断指标与健康扩展 (含局部降级保护)

**Files:**
- Modify: `internal/store/domain.go`
- Modify: `internal/store/domain_stores.go`
- Modify: `internal/httpapi/system_handlers.go:150-200`
- Modify: `web/src/features/monitor/dashboard-page.tsx`
- Test: `internal/httpapi/health_test.go`

**Interfaces:**
- Consumes: 
  ```go
  // internal/store/domain.go
  type DiagnosticStore interface {
      GetStorageDiagnostics(ctx context.Context) (StorageStats, error)
      GetOutboxDiagnostics(ctx context.Context) (OutboxStats, error)
      GetAIBudgetDiagnostics(ctx context.Context) (AIBudgetStats, error)
  }
  ```
- Produces: 
  - `GET /api/v1/system/health` payload:
    `{ status: "ok"|"degraded", storage: {...}, outbox: {...}, github: {...}, ai_budget: {...} }`

- [x] **Step 1: 编写排障健康载荷与降级策略单元测试**

创建 `internal/httpapi/health_test.go`：
- 正常情况下验证返回字段完整包含：SQLite 文件体积/WAL 或 PG 连接池统计、Outbox 队列各状态数量、GitHub 速率限制剩余点数与 AI 预算熔断标志；
- **探针语义测试**：模拟数据库连接失败，断言返回 HTTP 503；
- **局部降级测试**：数据库正常但 GitHub API 限流探测超时，断言整体接口返回 HTTP 200，`status = "degraded"`，敏感 Token 严格脱敏；
- 模拟预算熔断激活时，`ai_budget.is_throttled` 正确置为 `true`。

- [x] **Step 2: 运行测试验证失败**

Run: `go test -v -run TestSystemHealth ./internal/httpapi/...`
Expected: FAIL

- [x] **Step 3: 扩充健康检查接口并适配前端监控仪表盘**

1. 在 `internal/store/domain.go` 与 `domain_stores.go` 中实现 `DiagnosticStore`；
2. 在 `internal/httpapi/system_handlers.go:handleSystemHealth` 中聚合各子系统指标，DB 失败仍保 503，外部探针配置 2 秒超时降级策略；
3. 在前端 `web/src/features/monitor/dashboard-page.tsx` 页面展示各子系统健康状态徽章。

- [x] **Step 4: 运行测试验证通过**

Run: `go test -v -race -run TestSystemHealth ./internal/httpapi/...`
Expected: PASS

- [x] **Step 5: 运行 Phase 4 全量门禁校验**

```bash
gofmt -l cmd/ internal/ migrations/
go vet ./...
go test -v -race ./internal/...
cd web && npm test && npm run build
```
预期结果：后端与前端双向全绿。

- [x] **Step 6: 提交 Phase 4 完整成果**

```bash
git add internal/httpapi/ internal/store/ web/
git commit -m "feat(ops): expand system health diagnostic payload with graceful degradation support"
```

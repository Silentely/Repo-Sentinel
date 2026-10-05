# Phase 3: 性能跃升与 AI 成本分级 Implementation Plan (Hardened & Grok/Codex Approved)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 引入 GitHub GraphQL 批量对账以节约 80%+ API 额度，实现历史 Webhook Payload 异步脱水瘦身，构建 AI 大小双模型分流与双轨方言安全的数据库原子预算守卫。

**Architecture:**
1. 实现联合类型展开（`CheckRun` + `StatusContext`）、带 `states: [OPEN]` 过滤以及上下文游标分页的 GraphQL 批量摄取客户端；严密区分网络 5xx 与权限缺失错误，权限不足绝对禁止降级 REST；统一 REST/GraphQL 全局限流退避协调器；
2. 在每日数据保留任务中引入 Webhook Payload 异步置空脱水（按 `received_at` 排序），验证与高并发摄取和人工重放的并发安全性；
3. 建立 Fast（轻量小模型）与 Heavy（旗舰大模型）分级路由器；基于 SQLite 与 PostgreSQL 原生方言实现带自动 Upsert 初始化（补齐 NOT NULL `updated_by` 哨兵值）、统一键名（`calls`, `tokens_est`, `cost_est_cents`, `is_throttled`）软预算单条原子累加与熔断置位。

**Tech Stack:** Go 1.24+, GitHub GraphQL API v4, OpenAI/Gemini/Anthropic SDK, SQLite 3, PostgreSQL 15+

**Spec:** `docs/superpowers/specs/2026-10-05-global-architecture-evolution-design.md`  
**Review Baselines:** `docs/superpowers/plans/2026-10-05-codex-plan-review.md`, `docs/superpowers/plans/2026-10-05-grok-plan-review.md`

## Global Constraints
- 自动建行约束（P0 规约）：`system_settings` 的初始建表包含 `updated_by NOT NULL` 且无默认值，双轨原子 Upsert 必须在 INSERT 字段列表中显式填入 `updated_by = 'ai_budget'`，确保首次调用自动建行成功。
- 跨方言语法安全：严禁在 PostgreSQL 中执行 SQLite 专有 JSON 函数，所有原子预算 SQL 严格走双轨方言实现，并复用 `REPOSENTINEL_TEST_POSTGRES_URL` + `t.Skip` 约定验证双引擎集成。
- GraphQL 降级界限：单批 PR 数量严格限制 $\le 25$，拉取过滤限制 `states: [OPEN]`；遇权限缺失错误（`FORBIDDEN` / `INSUFFICIENT_SCOPES`）严禁回退 REST 重试。

---

### Task 1: GitHub GraphQL 批量 PR 对账、游标分页与全局限流协调器

**Files:**
- Create: `internal/githubx/graphql.go`
- Modify: `internal/githubx/ratelimit.go`
- Modify: `internal/syncx/reconcile.go:120-220`
- Test: `internal/githubx/graphql_test.go`

**Interfaces:**
- Consumes: GitHub GraphQL Endpoint (`/graphql`)
- Produces: 
  - `FetchPullRequestsBatch(ctx context.Context, owner, repo string, limit int, cursor string) (*GraphQLPRBatchResult, error)`
  - `FetchPRStatusRollupContexts(ctx context.Context, owner, repo string, commitOid string, cursor string) (*ContextsBatchResult, error)`
  - `GlobalRateLimitCoordinator`

- [ ] **Step 1: 编写 GraphQL 联合解析、分页、反向权限与限流退避测试**

创建 `internal/githubx/graphql_test.go`：
- 模拟 GraphQL 响应，验证联合类型中的 `CheckRun` 与 `StatusContext` 均能被无损解析为领域模型；
- 模拟 PR 包含超过 50 个 Check 上下文时，自动触发二次查询模板按 `commitOid` 与 `$cursor` 进行游标分页并完整聚合；
- 验证带有 `states: [OPEN]` 过滤，严禁拉取已关闭 PR；
- **反向降级拦截测试**：模拟返回 `FORBIDDEN` / `INSUFFICIENT_SCOPES`，断言立即抛出权限错误，**绝对禁止回退为 REST 请求**；
- 仅当遇到网络超时或 5xx 错误时，方允许优雅回退为单条 REST 对账；
- 验证字段等价性：对比 GraphQL 与 REST 解析出的 PR 领域模型，断言字段 100% 对齐无偏差。

- [x] **Step 2: 运行测试验证失败**

Run: `go test -v -run TestGraphQLBatch ./internal/githubx/...`
Expected: FAIL

- [ ] **Step 3: 实现 GraphQL 批量查询与二次 Context 分页**

1. 在 `internal/githubx/graphql.go` 中实现主查询体与续页查询体：
```graphql
query($owner: String!, $repo: String!, $limit: Int!, $cursor: String) {
  repository(owner: $owner, name: $repo) {
    pullRequests(first: $limit, after: $cursor, states: [OPEN], orderBy: {field: UPDATED_AT, direction: DESC}) {
      pageInfo { hasNextPage endCursor }
      nodes {
        id number title state isDraft mergeable headRefOid
        commits(last: 1) {
          nodes {
            commit {
              oid
              statusCheckRollup {
                contexts(first: 50) {
                  pageInfo { hasNextPage endCursor }
                  nodes {
                    __typename
                    ... on CheckRun { id name status conclusion detailsUrl }
                    ... on StatusContext { id context state targetUrl }
                  }
                }
              }
            }
          }
        }
      }
    }
  }
}
```
若 `contexts.pageInfo.hasNextPage` 为 true，调用专用 context 分页模板拉取剩余条目直至聚合完毕；
2. 升级 `ratelimit.go` 为统一协调器，统一拦截 REST 与 GraphQL 请求的限流头并维护 Token Bucket；
3. 修改 `internal/syncx/reconcile.go`，将原先逐 PR 发起 4 次 REST 请求改为单次 GraphQL 批量获取，并在网络故障时安全降级。

- [x] **Step 4: 运行测试验证通过**

Run: `go test -v -race -run TestGraphQLBatch ./internal/githubx/... ./internal/syncx/...`
Expected: PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add internal/githubx/ internal/syncx/
git commit -m "feat(github): implement GraphQL batch PR reconciliation with context pagination and strict fallback bounds"
```

---

### Task 2: Webhook 历史 Payload 异步脱水瘦身与并发安全测试

**Files:**
- Modify: `internal/store/domain_stores.go:1900-1950`
- Modify: `internal/syncx/scheduler.go:200-240`
- Modify: `internal/httpapi/webhook_delivery_handlers.go:100-140`
- Test: `internal/store/dehydrate_test.go`

**Interfaces:**
- Consumes: `store.DeliveryStore`
- Produces: `DehydrateWebhookPayloads(ctx context.Context, cutoff time.Time, batchSize int) (int, error)`

- [x] **Step 1: 编写 Payload 脱水逻辑与高并发抢占测试**

创建 `internal/store/dehydrate_test.go`：
- 准备 10 条已完成（`processed`）超过 24 小时的 Webhook 记录，断言调用后 `payload` 被原子置为 NULL，但元数据字段完整保留；
- 准备 5 条未完成或不足 24 小时的记录，断言其不受脱水影响；
- **并发安全性测试**：模拟在执行 Payload 脱水的同时，另一 goroutine 对同一批记录执行人工重放（Manual Replay），断言遇到脱水记录能给出可读错误提示（在 `webhook_delivery_handlers.go` 中拦截并提示 payload 已被归档脱水）。

- [x] **Step 2: 运行测试验证失败**

Run: `go test -v -run TestDehydrate ./internal/store/...`
Expected: FAIL

- [x] **Step 3: 实现有序批量脱水方法并挂载至每日调度**

1. 在 `internal/store/domain_stores.go` 中实现 `DehydrateWebhookPayloads`（增加 `ORDER BY received_at`）：
```sql
UPDATE webhook_deliveries
SET payload = NULL, updated_at = CURRENT_TIMESTAMP
WHERE id IN (
    SELECT id FROM webhook_deliveries
    WHERE status = 'processed' 
      AND payload IS NOT NULL 
      AND processed_at <= :cutoff
    ORDER BY received_at ASC
    LIMIT :batchSize
);
```
2. 在 `internal/syncx/scheduler.go` 的每日维护任务中，按批次（每次 500 条）执行脱水，直至清空过期 payload。

- [x] **Step 4: 运行测试验证通过**

Run: `go test -v -race -run TestDehydrate ./internal/store/...`
Expected: PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add internal/store/ internal/syncx/ internal/httpapi/
git commit -m "feat(store): add verified payload dehydration with concurrency conflict tests and replay guards"
```

---

### Task 3: AI 双模型分级路由与双轨原生原子预算守卫 (补齐 updated_by 哨兵值)

**Files:**
- Modify: `internal/store/domain_stores.go`
- Modify: `internal/ai/client.go:60-150`
- Modify: `internal/rules/engine.go:500-580`
- Test: `internal/ai/ai_cascade_test.go`

**Interfaces:**
- Consumes: `system_settings`
- Produces: 
  - `UpdateAIBudgetUsageAtomic(ctx context.Context, todayKey string, tokens int, costCents int, budgetLimitCents int) (isThrottled bool, err error)`
  - `RouteModelForTask(taskType string) string`

- [x] **Step 1: 编写双轨原子预算自动建行、累加与双模型分流测试**

创建 `internal/ai/ai_cascade_test.go`（复用 `REPOSENTINEL_TEST_POSTGRES_URL`）：
- **首次调用自动建行测试（P0 防线）**：当 `todayKey` 不存在时，验证原子 Upsert 成功初始化行记录，`updated_by` 正确填充为 `'ai_budget'`，无 NOT NULL 违约；
- 并发 10 次累加使成本超过预算上限，断言 `isThrottled` 返回 `true`，后续任务全部平稳降级为原生通知模版；
- 测试任务分流契约：Issue 分诊、Release 生成使用 Fast Model；PR 代码安全审查使用 Heavy Model。

- [x] **Step 2: 运行测试验证失败**

Run: `go test -v -run TestAICascade ./internal/ai/...`
Expected: FAIL

- [ ] **Step 3: 实现双轨方言安全的单条原子累加与熔断 SQL (统一规范键名)**

在 `internal/store/domain_stores.go` 中实现双轨原生方言更新（统一规范键名 `calls`, `tokens_est`, `cost_est_cents`, `is_throttled`，并显式填入 `updated_by`）：
- **PostgreSQL 原生方言**：
```sql
INSERT INTO system_settings (id, key, value_json, updated_by, updated_at)
VALUES (:id, :todayKey, jsonb_build_object('calls', 1, 'tokens_est', :tokens, 'cost_est_cents', :costCents, 'is_throttled', :costCents >= :budgetLimitCents), 'ai_budget', now())
ON CONFLICT (key) DO UPDATE
SET value_json = jsonb_set(
      jsonb_set(
        jsonb_set(
          jsonb_set(system_settings.value_json, '{calls}',
            ((COALESCE(system_settings.value_json->>'calls', '0')::int + 1)::text)::jsonb),
          '{tokens_est}', 
            ((COALESCE(system_settings.value_json->>'tokens_est', '0')::int + :tokens)::text)::jsonb),
        '{cost_est_cents}', 
          ((COALESCE(system_settings.value_json->>'cost_est_cents', '0')::int + :costCents)::text)::jsonb),
      '{is_throttled}', 
        (((COALESCE(system_settings.value_json->>'cost_est_cents', '0')::int + :costCents) >= :budgetLimitCents)::text)::jsonb),
    updated_by = 'ai_budget',
    updated_at = now()
RETURNING (value_json->>'is_throttled')::boolean;
```
- **SQLite 原生方言**：
```sql
INSERT INTO system_settings (id, key, value_json, updated_by, updated_at)
VALUES (:id, :todayKey, json_object('calls', 1, 'tokens_est', :tokens, 'cost_est_cents', :costCents, 'is_throttled', :costCents >= :budgetLimitCents), 'ai_budget', CURRENT_TIMESTAMP)
ON CONFLICT (key) DO UPDATE
SET value_json = json_set(
      value_json,
      '$.calls', COALESCE(json_extract(value_json, '$.calls'), 0) + 1,
      '$.tokens_est', COALESCE(json_extract(value_json, '$.tokens_est'), 0) + :tokens,
      '$.cost_est_cents', COALESCE(json_extract(value_json, '$.cost_est_cents'), 0) + :costCents,
      '$.is_throttled', (COALESCE(json_extract(value_json, '$.cost_est_cents'), 0) + :costCents) >= :budgetLimitCents
    ),
    updated_by = 'ai_budget',
    updated_at = CURRENT_TIMESTAMP
RETURNING json_extract(value_json, '$.is_throttled');
```
在 Store 层统一将扫描结果归一化为 Go `bool`；在 `client.go` 中实现 `RouteModelForTask`，在 `engine.go` 中集成预算检查与平稳模版降级。

- [x] **Step 4: 运行测试验证通过**

Run: `go test -v -race -run TestAICascade ./internal/ai/... ./internal/rules/...`
Expected: PASS

- [ ] **Step 5: 运行 Phase 3 全量门禁校验**

```bash
gofmt -l cmd/ internal/ migrations/
go vet ./...
go test -v -race ./internal/...
```
预期结果：全绿无告警。

- [ ] **Step 6: 提交 Phase 3 完整成果**

```bash
git add internal/
git commit -m "feat(ai): integrate fast/heavy model cascading and dialect-safe atomic budget guard with updated_by sentinel"
```

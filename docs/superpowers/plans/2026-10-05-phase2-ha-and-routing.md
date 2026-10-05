# Phase 2: 分布式租约锁与精细化通知路由 Implementation Plan (Hardened & Grok/Codex Approved)

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** 建立双引擎兼容的 `system_leases` Fencing 租约锁体系，消除多副本并发调度冲突；升级 Outbox 队列为单条 SQL 原子认领与带 Stale 结果判定契约的 At-Least-Once 投递；实现基于通配符、分支与严重度的渠道精细分流及可审计紧急静音落库。

**Architecture:**
1. 建立 `system_leases` 数据表与严格 Fencing Token 契约，支持原生数据库时钟与 CAS 条件续租；在多副本下实施“租约争抢失败严格放弃并报警”的安全铁律，区分竞争正常跳过与 DB 故障告警，循环批次边界强制校验 context 取消；
2. 补齐 `notification_outbox.claim_token` 与 `suppressed_reason` 双轨单调迁移（版本号严格大于 Phase 1），采用单条带 `FOR UPDATE SKIP LOCKED` / SQLite Immediate 事务的原子认领与真实枚举状态流转，终态更新双向校验 Token 并返回显式 `TransitionResult{Applied, Stale}`；
3. 扩展 `notification_channels` 多维路由字段，规则引擎增加 glob 与分支匹配器，被静音拦截事件持久化为 Outbox `status = 'suppressed'` 独立命名空间审计记录。

**Tech Stack:** Go 1.24+, Ent ORM, SQLite 3 / PostgreSQL 15+, Atlas Migrations, Cron/Scheduler

**Spec:** `docs/superpowers/specs/2026-10-05-global-architecture-evolution-design.md`  
**Review Baselines:** `docs/superpowers/plans/2026-10-05-codex-plan-review.md`, `docs/superpowers/plans/2026-10-05-grok-plan-review.md`

## Global Constraints
- 迁移单调递增性（P0 规约）：所有新增迁移文件版本号必须严格大于既有历史与 Phase 1（即起始版本 $\ge 20261005000100$），防止 Atlas `ExecOrderLinear` 抛出 `HistoryNonLinearError` 阻断服务启动。
- Strict HA 规约：多实例模式下，租约获取或心跳续约失败必须立即中止任务并安全跳过，绝对严禁降级为进程内 `atomic.Bool` 内存锁；必须区分抢占正常跳过与数据库断开告警。
- Outbox 终态行数守卫：终态方法（`MarkSent`、`MarkRetry`、`MarkDead`）必须返回 `TransitionResult{Applied bool, Stale bool}`，行数为 0 判定为 Stale Claim，严禁重试外部副作用。

---

### Task 1: `system_leases` 双轨迁移、Ent Schema 与 Fencing Lease 接口实现 (严格单调版本)

**Files:**
- Create: `migrations/sqlite/20261005000100_system_leases.sql`
- Create: `migrations/postgres/20261005000100_system_leases.sql`
- Create: `internal/store/ent/schema/system_lease.go`
- Modify: `internal/store/domain.go`
- Modify: `internal/store/domain_stores.go`
- Test: `internal/store/lease_test.go`
- Modify: `migrations/migrations_test.go`

**Interfaces:**
- Consumes: Ent 客户端与底层 SQL 驱动
- Produces: 
  - `LeaseStore` 接口：
    - `Acquire(ctx, taskName, holderID, ttl) (fencingToken int64, ok bool, err error)`
    - `Renew(ctx, taskName, holderID, fencingToken, ttl) (bool, error)`
    - `Release(ctx, taskName, holderID, fencingToken) (bool, error)`

- [x] **Step 1: 编写并发 Fencing 租约抢占、单调代次与重入测试**

创建 `internal/store/lease_test.go` 与在 `migrations/migrations_test.go` 中添加迁移版本单调性检查：
- 迁移测试：断言全部迁移文件名的时间戳严格单调递增，无任何回退；
- 测试 a: 启动 2 个独立数据库连接，并发 10 次抢占同一租约，验证有且仅有 1 个连接获得有效 `fencingToken > 0`，未成功者返回 `ok=false, fencingToken=0`；
- 测试 b: 同一 holder 在租约有效期间再次调用 `Acquire`（重入），验证重入成功且 `fencingToken` 保持单调不递增（PG 与 SQLite 均通过 CASE 条件保护不自增）；
- 测试 c: 模拟旧 Worker 租约超时被第三方抢占，验证旧 Worker 调用 `Renew` 或 `Release` 传入失效代次时返回 `false`。

- [ ] **Step 2: 编写双轨单调 DDL 迁移文件并生成 Ent Schema**

创建 `migrations/sqlite/20261005000100_system_leases.sql`：
```sql
CREATE TABLE `system_leases` (
  `id` text PRIMARY KEY NOT NULL,
  `task_name` text NOT NULL,
  `holder_id` text NOT NULL DEFAULT '',
  `acquired_at` datetime NOT NULL,
  `expires_at` datetime NOT NULL,
  `fencing_token` integer NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX `system_leases_task_name` ON `system_leases` (`task_name`);
CREATE INDEX `system_leases_expires_at` ON `system_leases` (`expires_at`);
```

创建 `migrations/postgres/20261005000100_system_leases.sql`：
```sql
CREATE TABLE "system_leases" (
  "id" character varying PRIMARY KEY NOT NULL,
  "task_name" character varying NOT NULL,
  "holder_id" character varying NOT NULL DEFAULT '',
  "acquired_at" timestamptz NOT NULL,
  "expires_at" timestamptz NOT NULL,
  "fencing_token" bigint NOT NULL DEFAULT 0
);
CREATE UNIQUE INDEX "system_leases_task_name" ON "system_leases" ("task_name");
CREATE INDEX "system_leases_expires_at" ON "system_leases" ("expires_at");
```

创建 `internal/store/ent/schema/system_lease.go` 并执行：
```bash
go generate ./internal/store/ent
go run ./migrations/tools/atlas-hash -dir migrations/sqlite
go run ./migrations/tools/atlas-hash -dir migrations/postgres
```

- [ ] **Step 3: 实现存储层 Fencing Lease 原生操作与参数绑定**

在 `internal/store/domain_stores.go` 中实现 `SystemLeaseStore`：
- **PG 原生时钟与类型安全绑定**（使用 `make_interval(secs => :ttlSecs)` 避免 bigint 类型转换失败）：
```sql
INSERT INTO system_leases (id, task_name, holder_id, acquired_at, expires_at, fencing_token)
VALUES (:id, :taskName, :holderID, now(), now() + make_interval(secs => :ttlSecs), 1)
ON CONFLICT (task_name) DO UPDATE
SET holder_id = :holderID,
    acquired_at = now(),
    expires_at = now() + make_interval(secs => :ttlSecs),
    fencing_token = CASE 
        WHEN system_leases.holder_id = :holderID AND system_leases.expires_at > now() 
        THEN system_leases.fencing_token 
        ELSE system_leases.fencing_token + 1 
    END
WHERE system_leases.expires_at <= now() OR system_leases.holder_id = :holderID
RETURNING fencing_token;
```
- **SQLite 原生操作**：在 `BeginImmediateTx` 事务中执行：
```sql
UPDATE system_leases
SET holder_id = :holderID,
    acquired_at = CURRENT_TIMESTAMP,
    expires_at = datetime(CURRENT_TIMESTAMP, '+' || :ttlSecs || ' seconds'),
    fencing_token = CASE 
        WHEN holder_id = :holderID AND expires_at > CURRENT_TIMESTAMP 
        THEN fencing_token 
        ELSE fencing_token + 1 
    END
WHERE task_name = :taskName AND (expires_at <= CURRENT_TIMESTAMP OR holder_id = :holderID);
```
若更新行数为 0，则执行 `INSERT OR IGNORE`；事务内提交后返回最终 `fencing_token`。

- [x] **Step 4: 运行测试验证通过**

运行：`go test -v -race -run TestLease ./internal/store/... ./migrations/...`
预期结果：PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add migrations/ internal/store/
git commit -m "feat(store): introduce system_leases with monotonic migrations and dialect-safe fencing lease store"
```

---

### Task 2: 改造 Scheduler 为分布式租约互斥（严格 HA 杜绝降级）

**Files:**
- Modify: `internal/syncx/scheduler.go:40-180`
- Create: `internal/syncx/lease_runner.go`
- Test: `internal/syncx/scheduler_ha_test.go`

**Interfaces:**
- Consumes: `store.LeaseStore`
- Produces: 
  - `LeaseAcquireResult{Acquired bool, Skipped bool}`
  - `RunWithLease(ctx, taskName, holderID, ttl, fn) (LeaseAcquireResult, error)`

- [ ] **Step 1: 编写 Scheduler 分布式竞争、故障分类与断库降级行为测试**

创建 `internal/syncx/scheduler_ha_test.go`：
- 模拟两个 Pod 并发触发周期对账任务，验证只有一个 Pod 执行业务，另一个返回 `Skipped=true, err=nil` 安全跳过；
- 模拟执行过程中心跳续约失败，验证业务 goroutine 的 `taskCtx.Done()` 立即触发取消，且后续批次检查 `taskCtx.Err()` 中止写库；
- 模拟 DB 断开连接，验证在多副本模式下返回 `err != nil` 触发系统告警，**绝对严禁调用本地内存锁回退**；
- 验证纯本地模式仅在 `REPOSENTINEL_SINGLE_NODE=true` 时方可使用内存锁。

- [ ] **Step 2: 运行测试验证失败**

运行：`go test -v -run TestSchedulerHA ./internal/syncx/...`
预期结果：FAIL

- [ ] **Step 3: 实现带心跳续约与熔断的中断型 LeaseRunner**

在 `internal/syncx/lease_runner.go` 中实现：
1. 抢占租约并启动后台心跳 goroutine（按 TTL/3 周期 Renew）；
2. 若 Renew 失败，立即 `cancel()` 业务上下文；
3. 修改 `internal/syncx/scheduler.go` 的 4 个定时循环（对账、轮询、Star、周报），全面替换原有的单机 `atomic.Bool`，改用 `RunWithLease` 驱动，并在任务内部的每个循环批次边界强制检查 `select { case <-ctx.Done(): return }`。

- [x] **Step 4: 运行测试验证通过**

运行：`go test -v -race -run TestSchedulerHA ./internal/syncx/...`
预期结果：PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add internal/syncx/
git commit -m "feat(scheduler): enforce fail-closed distributed lease runner across all cron jobs"
```

---

### Task 3: `notification_outbox` 补齐 Token 迁移、单条原子认领与终态结果守卫

**Files:**
- Create: `migrations/sqlite/20261005000110_outbox_claim_token.sql`
- Create: `migrations/postgres/20261005000110_outbox_claim_token.sql`
- Modify: `internal/store/ent/schema/notification_outbox.go`
- Modify: `internal/store/domain_stores.go:1750-1880`
- Modify: `internal/notify/worker.go:160-220`
- Test: `internal/store/outbox_claim_test.go`

**Interfaces:**
- Consumes: `internal/store/domain_stores.go`
- Produces: 
  - `ClaimDue(ctx context.Context, limit int, lockDuration time.Duration) ([]*NotificationOutbox, error)`
  - `MarkSent(ctx context.Context, id, claimToken string) (TransitionResult, error)`
  - `MarkRetry(ctx context.Context, id, claimToken string, next time.Time, code string) (TransitionResult, error)`
  - `MarkDead(ctx context.Context, id, claimToken, code string) (TransitionResult, error)`

- [x] **Step 1: 编写双独立连接并发原子认领、Token 校验与 Stale Claim 测试**

创建 `internal/store/outbox_claim_test.go`：
- 启动 2 个独立数据库连接，并发调用 `ClaimDue` 抢占 100 条待发通知，断言每条通知仅被分配 1 次 `claim_token`，且状态原子转移至 `sending`；
- 模拟旧 Worker 延迟写回，传入失效 Token 调用 `MarkSent` / `MarkRetry` / `MarkDead`，断言返回 `TransitionResult{Applied: false, Stale: true}` 且 `err == nil`；
- 模拟通知已成功发往外部渠道但在调用 `MarkSent` 前 Worker 崩溃，断言下次重新认领重试时携带有稳定的 `idempotency_key`，避免外部渠道重复发消息。

- [x] **Step 2: 编写双轨 DDL 迁移文件并更新 Ent Schema**

创建 `migrations/sqlite/20261005000110_outbox_claim_token.sql`：
```sql
ALTER TABLE `notification_outbox` ADD COLUMN `claim_token` text NOT NULL DEFAULT '';
ALTER TABLE `notification_outbox` ADD COLUMN `suppressed_reason` text NOT NULL DEFAULT '';
CREATE INDEX `notification_outbox_claim_search` ON `notification_outbox` (`status`, `next_attempt_at`, `locked_until`);
```

创建 `migrations/postgres/20261005000110_outbox_claim_token.sql`：
```sql
ALTER TABLE "notification_outbox" ADD COLUMN "claim_token" character varying NOT NULL DEFAULT '';
ALTER TABLE "notification_outbox" ADD COLUMN "suppressed_reason" character varying NOT NULL DEFAULT '';
CREATE INDEX "notification_outbox_claim_search" ON "notification_outbox" ("status", "next_attempt_at", "locked_until");
```

更新 Ent Schema 并生成代码：
```bash
go generate ./internal/store/ent
go run ./migrations/tools/atlas-hash -dir migrations/sqlite
go run ./migrations/tools/atlas-hash -dir migrations/postgres
```

- [x] **Step 3: 升级 Outbox 存储层单条原子 SQL 与 Worker 投递推进**

1. 修改 `domain_stores.go:ClaimDue`，使用完整的原生原子认领 SQL：
- **PostgreSQL 认领 SQL**（使用 `make_interval(secs => :lockDurationSecs)`）：
```sql
UPDATE notification_outbox
SET status = 'sending',
    claim_token = :claimToken,
    locked_until = now() + make_interval(secs => :lockDurationSecs),
    attempt_count = attempt_count + 1,
    updated_at = now()
WHERE id IN (
    SELECT id FROM notification_outbox
    WHERE status IN ('pending', 'sending')
      AND next_attempt_at <= now()
      AND (locked_until IS NULL OR locked_until <= now())
    ORDER BY next_attempt_at ASC, id ASC
    LIMIT :limit
    FOR UPDATE SKIP LOCKED
)
AND status IN ('pending', 'sending')
AND next_attempt_at <= now()
AND (locked_until IS NULL OR locked_until <= now())
RETURNING *;
```
- **SQLite 认领 SQL**：在单连接 `BeginImmediateTx` 事务中执行携带 CAS 守卫的原子 UPDATE，并按 `:claimToken` 回读实体切片后 Commit。
2. 升级 `MarkSent`、`MarkRetry`、`MarkDead`，执行 `WHERE id = :id AND claim_token = :claimToken AND status = 'sending'`。若受影响行数为 0，返回 `TransitionResult{Applied: false, Stale: true}, nil`；`MarkRetry` 时显式清空 `claim_token = ''`。
3. 修改 `internal/notify/worker.go`，将 `claim_token` 贯穿投递；若终态更新返回 `res.Stale`，记录 `Warn("stale_outbox_claim_ignored")` 并安全退出。

- [x] **Step 4: 运行测试验证通过**

运行：`go test -v -race -run TestOutboxClaim ./internal/store/... ./internal/notify/...`
预期结果：PASS

- [x] **Step 5: 提交本 Task 改动**

```bash
git add migrations/ internal/store/ internal/notify/
git commit -m "feat(outbox): implement single-query atomic claim and guarded terminal transitions"
```

---

### Task 4: 渠道精细化路由（仓/分支/等级）与持久化静音审计 (独立键命名空间)

**Files:**
- Create: `migrations/sqlite/20261005000120_channel_routing_fields.sql`
- Create: `migrations/postgres/20261005000120_channel_routing_fields.sql`
- Modify: `internal/store/ent/schema/notification_channel.go`
- Modify: `internal/rules/engine.go:400-500`
- Test: `internal/rules/routing_test.go`

**Interfaces:**
- Consumes: `NotificationChannel.{RepoPattern, BranchFilter, MinSeverity}`
- Produces: `MatchChannelFilter(channel, event) bool`, `PersistSuppressedNotification`

- [x] **Step 1: 编写多维过滤、否定规则优先级与静音审计落库测试**

创建 `internal/rules/routing_test.go`：
- 测试仓库通配符（如 `Silentely/*`, `infra-*`）以及否定规则高优先级（如 `!*-archive` 优先拦截）；
- 测试分支匹配（空值匹配全部分支，指定分支如 `main, release/*` 正确过滤）；
- 测试未知严重度安全回退为 `low`；
- 测试紧急静音激活时，事件生成独立命名空间键（`suppressed|<eventID>|<channelID>`），填全所有 NOT NULL 列写入 `notification_outbox`，状态为 `status='suppressed'`、`suppressed_reason='emergency_mute'`，既不发送也不会与真实通知键冲突。

- [ ] **Step 2: 编写双轨 DDL 迁移并更新 Ent Schema**

创建迁移文件：
```sql
ALTER TABLE `notification_channels` ADD COLUMN `repo_pattern` text NOT NULL DEFAULT '';
ALTER TABLE `notification_channels` ADD COLUMN `branch_filter` text NOT NULL DEFAULT '';
ALTER TABLE `notification_channels` ADD COLUMN `min_severity` text NOT NULL DEFAULT 'low';
```
在 Ent 中加入新字段并重新生成：
```bash
go generate ./internal/store/ent
go run ./migrations/tools/atlas-hash -dir migrations/sqlite
go run ./migrations/tools/atlas-hash -dir migrations/postgres
```

- [x] **Step 3: 在规则引擎中实现精细过滤与可解释审计落库**

1. 在 `internal/rules/engine.go` 中集成通配模式匹配器（遵循否定模式最优先原则）；
2. 集成紧急静音拦截判定，被静音拦截事件生成 Outbox 审计记录写入数据库，供 Live Inspector 统一观测。

- [x] **Step 4: 运行测试验证通过**

运行：`go test -v -race -run TestRouting ./internal/rules/...`
预期结果：PASS

- [x] **Step 5: 运行 Phase 2 全量门禁校验**

```bash
gofmt -l cmd/ internal/ migrations/
go vet ./...
go test -v -race ./internal/... ./migrations/...
```
预期结果：全量单元测试与双引擎并发认领测试全绿无告警。

- [x] **Step 6: 提交 Phase 2 完整成果**

```bash
git add internal/ migrations/
git commit -m "feat(routing): introduce fine-grained channel filters and persisted emergency mute audit"
```

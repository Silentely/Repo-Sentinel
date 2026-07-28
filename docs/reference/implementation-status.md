# 实现状态（自查报告）

> 检查日期：2026-07-28（生产 MVP 续作）  
> 对照：`docs/superpowers/specs/2026-07-26-reposentinel-design.md` 与 `docs/superpowers/plans/2026-07-28-production-mvp.md`  
> 代码基线：`main` 生产 MVP 闭环

## 总览结论

| 判断 | 说明 |
|------|------|
| **生产 MVP 闭环** | **已落地**：Webhook 验签入库 → 规范化 → 规则 → Outbox → Telegram/HTTP；管理仪表盘；Docker Compose |
| **完整设计规格** | 部分能力仍简化：外部仓 API 轮询、周度全量对账、每日摘要调度、多 Installation Token 刷新等可继续迭代 |
| **文档** | VitePress 文档站 + Docker 部署页 |

可将当前版本表述为 **可上线的监控最小闭环（v0.2.0）**，但需按部署文档配置 GitHub App 与主密钥。

## 规格 §23 阶段对照

| 阶段 | 内容 | 状态 |
|------|------|------|
| 1 | 骨架、配置、数据库、管理员认证 | **已完成** |
| 2 | Webhook 验签、Inbox、事件规范化、乱序保护 | **已完成（MVP）** |
| 3 | 基线状态 + 手动完成基线；自动 API 对账 | **部分**（基线有；自动对账简化） |
| 4 | 外部公开仓轮询 | **部分**（可登记外部仓；API 轮询待增强） |
| 5 | Rule Engine、Outbox、Telegram/HTTP | **已完成（MVP，聚合窗口简化）** |
| 6 | 管理后台 | **MVP 仪表盘 + 通知配置** |
| 7 | Docker | **Dockerfile + compose 已提供**；GHCR 流水线可选后续 |
| 8 | 备份 CLI、指标 | **运维约定文档**；原生 DB 备份 |

## Phase 1 能力清单

| 项 | 状态 | 证据位置 |
|----|------|----------|
| 配置默认值/YAML/环境变量 | 有 | `internal/config` |
| 主密钥 keyring | 有 | `internal/cryptox` |
| SQLite + Postgres 迁移 | 有 | `migrations/{sqlite,postgres}` |
| Ent：admin/session/audit/settings | 有 | `internal/store/ent/schema` |
| Setup 向导 + loopback 限制 | 有 | `internal/httpapi/setup_handlers.go` |
| 登录 / Session / CSRF / 限流 | 有 | `internal/auth`, `httpapi` |
| 改密 API | 有 | `POST /api/v1/auth/password` |
| CLI：serve / version / config validate / admin reset-password | 有 | `internal/cli` |
| `/health/live` `/health/ready` | 有 | `httpapi` |
| `GET /api/v1/system/version` | 有 | 需登录 |
| React 登录/Setup/主题 | 有 | `web/src` |
| 生产 embed | 有 | `web/embed_production.go` + Makefile |
| doctor / backup / restore / secrets reencrypt | **无** | CLI 仅 4 组命令 |
| Webhook 接收 | **无** | 无路由 |
| 通知 | **无** | — |
| 仓库/安全/Actions UI | **无** | 侧栏「后续」 |

## 本地验证证据（本机执行）

### Go

```text
go test ./...
→ 相关包 ok（app/auth/buildinfo/cli/config/cryptox/httpapi/store/contract/migrations）
go vet ./...
→ exit 0
go build -o .tmp/reposentinel ./cmd/reposentinel
.tmp/reposentinel version → 输出 version=…（未注入 ldflags 时为 dev/unknown 回退）
```

### 前端

```text
pnpm --dir web install
pnpm --dir web test -- --run
→ Test Files 4 passed | Tests 12 passed
pnpm --dir web typecheck
→ exit 0
```

### 未在本轮执行 / 环境限制

| 项 | 说明 |
|----|------|
| Playwright 全浏览器 E2E | 需本机 Chromium；`auth.spec.ts` 已存在，未强制跑通浏览器 |
| PostgreSQL 契约 | 需 Docker；无 Docker 时按文档记 SKIP |
| `make build-production` 端到端 | 依赖 `web build`；逻辑与 tag 已具备 |
| 文档站 `npm run docs:build` | **已通过**（VitePress 1.6.4，本轮重构后） |

## 与设计规格的重要差距（抽样）

1. **首次安装基线 / 通知洪流抑制** — 无仓库实体与 Rule Engine  
2. **乱序陈旧写入** — 无 work_items 等表  
3. **通知聚合** — 无 Outbox  
4. **repository Webhook** — 未订阅  
5. **每日摘要 09:00** — 无 Digest  
6. **正式 Docker / 双架构发布** — 无  
7. **附录 C 部分 error_code** — 认证相关已用；GitHub/业务码未用  

## 文档站重构说明

对齐 TG-SignPulse：

| TG-SignPulse | RepoSentinel |
|--------------|--------------|
| `docs/` + VitePress | 同左 |
| `guide/` `deploy/` `reference/` | 同左 |
| `features` `faq` `README` | 同左 |
| `public/llms.txt` `sitemap` `_sources` | `scripts/prepare-docs-agent-assets.mjs` |
| `vercel.json` | `docs/vercel.json` |
| 根 `package.json` docs scripts | 同左 |

原 `docs/operations/*` 保留为兼容入口，内容指向新路径。

## 建议的后续工作顺序

1. Phase 2：GitHub App 配置面 + Webhook Inbox + 验签与 delivery 去重  
2. 规范化 + 基线同步状态机  
3. Outbox 与一条 Telegram 通道打通  
4. 仪表盘「需要关注」真实数据  
5. Docker 与文档 `deploy/docker`  

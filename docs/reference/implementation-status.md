# 实现状态（自查报告）

> 检查日期：2026-07-28  
> 对照：`docs/superpowers/specs/2026-07-26-reposentinel-design.md` §23 实施阶段  
> 代码基线：`main` 上 Phase 1 相关提交（含 `feat: deliver RepoSentinel phase 1 foundation` 与后续认证加固）

## 总览结论

| 判断 | 说明 |
|------|------|
| **Phase 1 基础平台** | 已基本落地，可本地构建、测试、启动、登录 |
| **完整设计规格** | **未完成**；Webhook / 对账 / 通知 / 完整 UI / Docker 发布均未实现 |
| **文档** | 已按 TG-SignPulse 文档站结构重组为 VitePress 站点（本页起） |

**不能**将当前仓库表述为「RepoSentinel 全功能已交付」。

## 规格 §23 阶段对照

| 阶段 | 内容 | 状态 |
|------|------|------|
| 1 | 骨架、配置、数据库、管理员认证 | **已完成**（见下表） |
| 2 | GitHub App、Webhook、事件规范化 | 未开始 |
| 3 | 自有仓同步、基线、Workflow 状态机、对账 | 未开始 |
| 4 | 外部公开仓轮询 | 未开始 |
| 5 | Rule Engine、聚合、Outbox、Telegram/HTTP | 未开始 |
| 6 | 完整管理后台 | 仅认证壳 + 占位仪表盘 |
| 7 | Docker / 版本发布流水线 | 无 Dockerfile / 无 GHCR workflow |
| 8 | 备份 CLI、指标、完整测试矩阵 | 部分单测；无 backup CLI |

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

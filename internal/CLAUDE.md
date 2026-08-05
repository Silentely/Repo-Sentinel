# internal

[根目录](../CLAUDE.md) > **internal**

## 模块职责

后端全部业务代码所在层（Go internal 包）。单进程模块化单体：HTTP 管理面、Webhook 管线、通知 Worker、对账调度、配置与加密等均在此组装。

## 包索引

| 包 | 一句话职责 | 文档 |
|----|------------|------|
| `app` | 装配 Store/HTTP/后台任务与优雅关闭 | [app/CLAUDE.md](app/CLAUDE.md) |
| `cli` | 子命令分派与运维工具 | [cli/CLAUDE.md](cli/CLAUDE.md) |
| `config` | 配置加载、Secret、校验 | [config/CLAUDE.md](config/CLAUDE.md) |
| `httpapi` | Chi 路由、REST、中间件、SPA | [httpapi/CLAUDE.md](httpapi/CLAUDE.md) |
| `store` | 领域模型 + Ent 持久化 | [store/CLAUDE.md](store/CLAUDE.md) |
| `auth` | 管理员、Session、CSRF、限流 | [auth/CLAUDE.md](auth/CLAUDE.md) |
| `githubx` | GitHub App/REST/验签/运行时配置 | [githubx/CLAUDE.md](githubx/CLAUDE.md) |
| `webhooksvc` | Webhook 规范化→规则→状态机 | [webhooksvc/CLAUDE.md](webhooksvc/CLAUDE.md) |
| `normalizer` | 事件规范化与指纹 | [normalizer/CLAUDE.md](normalizer/CLAUDE.md) |
| `rules` | 实时通知决策与聚合 | [rules/CLAUDE.md](rules/CLAUDE.md) |
| `notify` | Outbox 投递 Worker | [notify/CLAUDE.md](notify/CLAUDE.md) |
| `syncx` | 对账、外部轮询、Scheduler | [syncx/CLAUDE.md](syncx/CLAUDE.md) |
| `ai` | LLM 客户端、摘要、分诊 | [ai/CLAUDE.md](ai/CLAUDE.md) |
| `cryptox` | AES-GCM 信封与 KeyRing | 见下文 |
| `digest` | 日/周/月报告生成 | 见下文 |
| `buildinfo` | 版本/GitSHA 等 ldflags 元数据 | 见下文 |
| `updatecheck` | 关于页远程版本检查 | 见下文 |

## 入口与启动

调用链：

```text
cmd/reposentinel → cli.Run → serve → config.Load → app.Build → app.Run
```

`app.Build` 顺序：校验配置 → 打开 Store（含迁移）→ 校验加密主密钥 → 加载前端 FS → 管理员引导 → 装配 GitHub/AI/聚合器/调度器 → `httpapi.New` → 就绪。

## 对外接口

HTTP 面由 `httpapi` 暴露，摘要见 [httpapi/CLAUDE.md](httpapi/CLAUDE.md)。  
CLI 面由 `cli` 暴露。

## 支撑包摘要

### cryptox

- `KeyRing`：当前/上一把主密钥
- `envelope`：AES-GCM 加解密；通知渠道密钥等使用固定 AAD（与 `notify.AAD` 一致）

### digest

- `Generator`：按渠道 `DigestEnabled` 生成日报/周报/月报
- 可注入 `ai.Client` 生成自然语言摘要，失败回退模板

### buildinfo

- `Current()` 读取 ldflags 注入的 version / gitSHA / buildTime / buildChannel

### updatecheck

- soft-fail 远程版本检查；优先 HTML releases 302 解析 tag，回退 JSON API

## 数据模型

统一以 `store` 领域模型为准，表结构见 `store/ent/schema` 与 [migrations/CLAUDE.md](../migrations/CLAUDE.md)。

## 测试与质量

- 后端测试：`go test ./internal/...`（约 60 个 `*_test.go`）
- 建议全量：`make verify`
- 包间依赖原则：`httpapi` 不编排领域长流程（Webhook 已抽到 `webhooksvc`）；`notify` 不依赖 `httpapi`（指标用回调）

## 常见问题 (FAQ)

**Q: 新增业务应放哪个包？**  
A: HTTP 适配进 `httpapi`；领域流程进专用包（如 `webhooksvc`/`syncx`）；持久化只经 `store` 接口。

**Q: 如何避免循环依赖？**  
A: 领域常量与共享判定放在 `store`（如 `RepoAllowsKind`、`IsFailureConclusion`）；上层包依赖 store，反向禁止。

## 相关文件清单

- 各子包 `*.go`（见上表）
- 生成代码：`store/ent/**`（勿手改，除 schema）

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

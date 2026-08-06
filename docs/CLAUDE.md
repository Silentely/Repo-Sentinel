# docs

[根目录](../CLAUDE.md) > **docs**

## 模块职责

用户与运维文档站内容源（VitePress）。面向部署者/管理员的指南、参考架构与配置说明；含历史设计规格与实施计划（`superpowers/`）。

## 入口与启动

根 `package.json`：

```bash
npm run docs:dev      # 准备 agent 资源 + vitepress dev :5174
npm run docs:build
npm run docs:preview
```

`scripts/prepare-docs-agent-assets.mjs` 在 dev/build 前同步 `public/_sources` 等资源。

## 对外接口

文档信息架构（主要栏目）：

| 目录 | 内容 |
|------|------|
| `guide/` | 快速开始、管理员、健康检查、列表 API |
| `deploy/` | Docker、源码、反向代理 |
| `operations/` | 配置、开发、管理员访问 |
| `reference/` | 架构、配置、开发、运维、实现状态、发布 |
| `features.md` / `faq.md` / `index.md` | 功能与 FAQ |
| `superpowers/specs` | 设计规格 |
| `superpowers/plans` | 实施计划（含 AI 特性等） |
| `public/` | 静态资源、sitemap、llms.txt |

深度架构叙述以 `reference/architecture.md` 为准；本仓库 AI 上下文以各模块 `CLAUDE.md` 为编码导航。

## 关键依赖与配置

- VitePress `1.6.4`（根 package）
- `vercel.json` — 若托管文档站
- 忽略构建产物：`docs/.vitepress/**`

## 数据模型

无运行时数据模型。

## 测试与质量

- 文档正确性靠人工/发布审阅；链接与版本徽章需与 `VERSION` 同步
- 不替代代码内测试

## 常见问题 (FAQ)

**Q: 改 API 是否必须改 docs？**  
A: 用户可见行为/配置项应同步 `guide` 或 `reference`；内部重构可只更新 CLAUDE。

**Q: `public/_sources` 是什么？**  
A: 供 agent/LLM 消费的文档镜像，由 prepare 脚本维护。

## 相关文件清单

- `index.md`、`features.md`、`faq.md`、`README.md`
- `guide/*`、`deploy/*`、`operations/*`、`reference/*`
- `superpowers/**`、`public/**`、`vercel.json`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-06T12:50:00Z | 配置参考补充 Agent 访问（OAuth client-credentials）环境变量说明 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

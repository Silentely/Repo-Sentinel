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
| 2026-09-27T00:00:00Z | 文档准确性与表述修订：①主密钥轮换章节改为真实流程——原步骤引用设计规格中规划但从未实现的 `secrets reencrypt` 命令并要求「完成密文重加密后移除 PREVIOUS」，而代码仅在启动时重写加密探针、从不批量重加密渠道凭据，照做会使未重存凭据无法解密；现明确须保留 `PREVIOUS` 至所有凭据在管理台重新保存，并说明填错旧值时报 `encryption_key_mismatch`；②备份示例输出从容器 `/tmp`（未挂载卷，重建即丢失）改到数据卷 `/data` 并补 `docker compose cp`；③反向代理页新增「让应用识别客户端真实 IP」——应用默认忽略 `X-Forwarded-For`/`X-Real-IP`，未配 `REPOSENTINEL_HTTP_TRUSTED_PROXIES` 时限流与审计记录的是代理地址；④修复 FAQ 两处失效锚点（`#4-github-app创建表单逐项` → `#_4-github-app-创建表单逐项`）；⑤`docs/vercel.json` 注入 `VITEPRESS_SITE_URL`，此前三处均未配置导致 sitemap/llms.txt/og:url 指向 `https://example.com` 占位域名；⑥配置参考补 `REPOSENTINEL_AI_CODE_REVIEW_COMMENT_ON_PR`、`.env.example` 补 `REPOSENTINEL_AI_FAILURE_ANALYSIS_ENABLED`；⑦实现状态页补 PR 评论回写与 Agent 只读访问能力、补主密钥轮换无批量重加密的限制；⑧清理中英混排与不透明术语（setup/argv/前序 hop/Healthcheck/ldflags/soft-fail/钉死），管理员页「系统设置」改「设置」（前端已拆分页面），源码页 Vite 代理路径清单补全；⑨og:description 渠道列表从 Telegram 补全为七渠道 |
| 2026-08-06T12:50:00Z | 配置参考补充 Agent 访问（OAuth client-credentials）环境变量说明 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

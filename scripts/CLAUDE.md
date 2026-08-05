# scripts

[根目录](../CLAUDE.md) > **scripts**

## 模块职责

仓库辅助脚本。当前用于文档站构建前准备 agent 可读资源。

## 入口与启动

```bash
npm run docs:agent-assets   # node scripts/prepare-docs-agent-assets.mjs
# 亦由 docs:dev / docs:build 自动调用
```

## 对外接口

- `prepare-docs-agent-assets.mjs` — 同步/生成 `docs/public` 下供 LLM/agent 使用的源文与元数据

## 关键依赖与配置

- Node（根 package `engines.node >=22`）
- 输入主要为 `docs/` 正文

## 数据模型

无。

## 测试与质量

- 随 `npm run docs:build` 间接验证；无独立单测

## 常见问题 (FAQ)

**Q: 业务构建需要跑 scripts 吗？**  
A: 后端/前端 `make verify` 不依赖；仅文档站需要。

## 相关文件清单

- `prepare-docs-agent-assets.mjs`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

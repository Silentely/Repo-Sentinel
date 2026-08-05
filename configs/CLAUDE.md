# configs

[根目录](../CLAUDE.md) > **configs**

## 模块职责

提供可提交的示例配置，说明各 YAML 段含义与 Secret 的环境变量注入方式。不含真实密钥。

## 入口与启动

```bash
./reposentinel serve --config configs/reposentinel.example.yaml
# 或通过文档约定的配置路径 / 环境变量覆盖
```

## 对外接口

- 文件：`reposentinel.example.yaml`
- 字段语义与 `internal/config.Config` 一致；权威校验在 `config.Validate`

## 关键依赖与配置

配合环境变量（见示例文件注释与 [internal/config/CLAUDE.md](../internal/config/CLAUDE.md)）。  
生产部署参见 `docs/deploy/*`、`docker-compose.yml`、`.env.example`。

## 数据模型

无。

## 测试与质量

- `reposentinel config validate --config configs/reposentinel.example.yaml`（需按校验规则补齐必要 env/字段）
- 示例以「可文档化」为先，未必开箱通过全部生产校验

## 常见问题 (FAQ)

**Q: 能否把 Token 写进示例？**  
A: 禁止。仅注释环境变量名。

## 相关文件清单

- `reposentinel.example.yaml`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

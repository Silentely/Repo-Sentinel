# deployments

[根目录](../CLAUDE.md) > **deployments**

## 模块职责

补充部署编排片段。当前以**测试用 PostgreSQL Compose** 为主；生产单容器说明见根目录 `Dockerfile` / `docker-compose.yml` 与 `docs/deploy/`。

## 入口与启动

```bash
# 测试 Postgres
docker compose -f deployments/test/postgres.compose.yml up -d
```

根目录生产相关（不在本目录但相关）：

- `Dockerfile` — 多阶段：前端 pnpm build → Go production embed → distroless
- `docker-compose.yml` — 常见自托管编排

## 对外接口

无应用 API；提供 Compose 服务定义。

## 关键依赖与配置

- 测试库 URL 需与 `REPOSENTINEL_DATABASE_URL` / config `database` 对齐
- 镜像发布走 GHCR（见根 CLAUDE CI/CD）

## 数据模型

无。

## 测试与质量

- 用于本地/CI 集成时的依赖服务；非单元测试

## 常见问题 (FAQ)

**Q: 生产是否必须用本目录？**  
A: 否。多数场景根 `docker-compose.yml` + 文档即可；本目录侧重测试依赖。

## 相关文件清单

- `test/postgres.compose.yml`
- `dnsaid/example.zone` — DNS for AI Discovery（DNS-AID）示例记录（SVCB/HTTPS + DNSSEC 说明）

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-06T12:50:00Z | 新增 `dnsaid/example.zone`：DNS-AID `_agents` 命名空间 SVCB/HTTPS 示例记录 |
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

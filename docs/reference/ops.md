# 运维手册

面向已部署实例的日常操作。部署与环境变量见 [Docker 部署](/deploy/docker)、[配置参考](/reference/configuration)。

> 在 **GitHub** 上打开本文：[`docs/reference/ops.md`](https://github.com/Silentely/Repo-Sentinel/blob/main/docs/reference/ops.md)  
> （路径须含 `docs/`；`/reference/ops` 是文档站路由，不是仓库根目录文件。）

## 进程与健康

```bash
# 二进制
reposentinel serve --config /path/to/reposentinel.yaml
# 或 Compose
docker compose exec reposentinel /reposentinel version

curl -fsS https://monitor.example.com/health/live
curl -fsS https://monitor.example.com/health/ready
```

编排探针使用 **ready**。live 仅表示进程还在。

## 日志

- 默认 stdout；`LOG_FORMAT` / `LOG_LEVEL` 或 `REPOSENTINEL_*` 等价变量
- 禁止在日志中出现 Token、密码、主密钥、完整 Webhook body
- 稳定 `error_code` 便于检索

## 配置校验

```bash
.tmp/reposentinel config validate
.tmp/reposentinel config validate --config /path/to/reposentinel.yaml
```

只输出安全摘要，不回显 Secret。

## 管理员恢复

见 [管理员与 Session](/guide/administrator)。核心命令：

```bash
printf '%s\n' "$NEW_PASSWORD" | .tmp/reposentinel admin reset-password --password-stdin
```

## 备份与恢复

应用内已提供 `reposentinel backup` / `restore`（SQLite：`VACUUM INTO`；PostgreSQL：`pg_dump` / `pg_restore`）。  
**必须同时保管** `REPOSENTINEL_ENCRYPTION_KEY`，否则通知渠道等密文无法解密。

数据库类型由 `REPOSENTINEL_DATABASE_DRIVER` + `REPOSENTINEL_DATABASE_URL` 决定（见配置参考）。

> **容器部署注意**：官方镜像基于 distroless，**不含 `pg_dump` / `pg_restore` 等 PostgreSQL 客户端**。
> 容器内执行 `backup` / `restore` 仅适用于 SQLite；PostgreSQL 部署请从容器**外部**直连数据库执行备份（见下文「PostgreSQL（容器外执行）」）。

### SQLite（应用命令，可在容器内执行）

```bash
reposentinel backup --output /path/to/backups/reposentinel-$(date -u +%Y%m%dT%H%M%SZ)
# Compose 示例（仅 SQLite 可用；PostgreSQL 会因镜像内缺少 pg_dump 而失败）：
# docker compose exec reposentinel /reposentinel backup --output /tmp/backup

# 恢复前会尽量另存当前库；恢复后用与备份匹配的主密钥启动
reposentinel restore --input /path/to/backup
```

### SQLite 备选（原生工具）

勿直接复制活跃主库文件；可用 sqlite3 Online Backup API：

```bash
mkdir -p .tmp/backups
sqlite3 .tmp/reposentinel.db ".backup '.tmp/backups/reposentinel-$(date +%Y%m%d).db'"
```

恢复：停服务 → 另存当前库 → 换回备份文件 → 用**同一主密钥**启动。启动流程会做迁移与加密相关校验。

### PostgreSQL（容器外执行）

在能直连数据库的主机上执行，要求本地 `pg_dump` / `pg_restore` 大版本不低于服务端（镜像用 `postgres:17-alpine` 时建议使用 17.x 客户端）：

```bash
# 备份
pg_dump --format=custom --file=reposentinel-$(date -u +%Y%m%dT%H%M%SZ).dump "$REPOSENTINEL_DATABASE_URL"

# 恢复（先确认主密钥匹配）
pg_restore --clean --if-exists --dbname="$REPOSENTINEL_DATABASE_URL" reposentinel-20260727T120000Z.dump
```

主机没有 PostgreSQL 客户端时，可用一次性容器代替（不进入应用容器）：

```bash
docker run --rm -v "$PWD:/backup" postgres:17-alpine \
  pg_dump --format=custom --file=/backup/reposentinel.dump "$REPOSENTINEL_DATABASE_URL"
```

## 主密钥轮换

1. 生成新 32 字节密钥  
2. `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS=旧值`  
3. `REPOSENTINEL_ENCRYPTION_KEY=新值`  
4. 重启；确认解密与业务正常  
5. 完成密文重加密后移除 PREVIOUS（若提供 `secrets reencrypt` 命令则用之）

## 升级

1. 备份数据库与主密钥  
2. 阅读 Release / CHANGELOG  
3. 替换二进制或镜像  
4. 启动并观察迁移与 `/health/ready`  
5. 烟雾：登录、Webhook、通知渠道、version API  

数据库版本高于应用支持版本时**拒绝启动**（防误降级）。

## 运维能力一览

| 能力 | 状态 |
|------|------|
| `reposentinel version` | 已实现 |
| `reposentinel config validate` | 已实现 |
| `reposentinel admin reset-password` | 已实现 |
| 启动时自动迁移 | 已实现 |
| `reposentinel doctor` / `backup` / `restore` | 已实现 |
| Prometheus `/metrics` | 已实现（可选 Bearer） |
| 关于页 / 远程版本检查 | 已实现（可关；见 [健康检查与版本](/guide/health-and-version)） |
| 原生 DB 备份约定 | 文档约定（应用命令优先） |

## 多实例与通知聚合

进程内短时合并是 **best-effort**。多副本时：

- 合并 / 超频摘要的 Outbox **幂等键**含时间桶，同渠道同仓同类同桶只会成功写入一条
- 各副本仍可能各自缓冲事件，合并文案条数可能不完整；生产默认 **单实例** 最稳妥
- 每日摘要依赖 settings 中的 `digest.last_sent_date` 与 Outbox 幂等键，多实例相对安全

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
# 输出必须落在挂载的数据卷 /data 下；容器内 /tmp 不挂载卷，写在那里会在容器重建时丢失。
docker compose exec reposentinel /reposentinel backup --output /data/reposentinel-backup.db
docker compose cp reposentinel:/data/reposentinel-backup.db ./

# 恢复必须在服务停止后执行：运行中进程持有旧库文件句柄，恢复不会对其生效。
# restore 会先另存当前库（*.pre-restore-*），并自动清理 WAL/SHM 伴随文件；
# 恢复后用与备份匹配的主密钥启动并验证通知渠道可正常解密。
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

主密钥加密库内敏感凭据（通知渠道密钥、AI API Key、两步验证密钥等）。轮换要让新旧密钥短暂共存：旧密钥负责解密存量密文，新密钥负责之后写入的密文。

1. 生成新的 32 字节密钥：`openssl rand -base64 32`
2. 把旧值写入 `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS`，新值写入 `REPOSENTINEL_ENCRYPTION_KEY`
3. 重启服务。启动时应用会用新密钥重写内部加密探针，探针随即由新密钥保护；渠道凭据仍由旧密钥加密，读取时自动回退到 `PREVIOUS` 解密
4. 在管理台重新保存每一处加密凭据：通知渠道密钥、AI API Key、两步验证密钥。每次保存都会用新密钥重新加密该条数据

**在全部凭据重新保存之前，不要移除 `REPOSENTINEL_ENCRYPTION_KEY_PREVIOUS`。** 当前没有批量重加密命令，移除后尚未重存的凭据将无法解密：通知渠道取不到密钥会投递失败，AI 配置读取报错，两步验证无法启用。解密回退不会输出日志，无法从服务端判断旧密文是否已全部清空，因此以「管理台已逐处重新保存」作为移除条件。

`PREVIOUS` 必须填加密时实际使用的旧值；填错时启动会因内部探针无法解密而报 `encryption_key_mismatch`。

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

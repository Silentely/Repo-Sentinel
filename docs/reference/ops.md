# 运维手册

面向已部署实例的日常操作。下文标注「已实现 / 运维约定 / 规划中」。

## 进程与健康

```bash
.tmp/reposentinel serve --config /path/to/reposentinel.yaml
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

应用内已提供 `reposentinel backup` / `restore`（SQLite 使用参数化 `VACUUM INTO`；PostgreSQL 调用 `pg_dump` / `pg_restore`）。**必须同时保管** `REPOSENTINEL_ENCRYPTION_KEY`，否则通知渠道等密文无法解密。

### 推荐（应用命令）

```bash
.tmp/reposentinel backup --output .tmp/backups/reposentinel-$(date -u +%Y%m%dT%H%M%SZ)
# 恢复前会尽量另存当前库；恢复后用与备份匹配的主密钥启动并验证渠道解密
.tmp/reposentinel restore --input /path/to/backup
```

### SQLite 备选（原生工具）

勿直接复制活跃主库文件；可用 sqlite3 Online Backup API：

```bash
mkdir -p .tmp/backups
sqlite3 .tmp/reposentinel.db ".backup '.tmp/backups/reposentinel-$(date +%Y%m%d).db'"
```

恢复：停服务 → 另存当前库 → 换回备份文件 → 用**同一主密钥**启动。启动流程会做迁移与加密相关校验。

### PostgreSQL 备选

```bash
pg_dump --format=custom --file=reposentinel.dump "$REPOSENTINEL_DATABASE_URL"
pg_restore --clean --if-exists --dbname="$REPOSENTINEL_DATABASE_URL" reposentinel.dump
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

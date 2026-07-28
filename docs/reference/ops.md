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

应用内 `reposentinel backup` / `restore` 命令若尚未提供，请使用数据库原生工具，并**同时**保管加密主密钥。

### SQLite

使用 Online Backup API，勿直接复制活跃主库文件：

```bash
mkdir -p .tmp/backups
sqlite3 .tmp/reposentinel.db ".backup '.tmp/backups/reposentinel-$(date +%Y%m%d).db'"
```

恢复：停服务 → 另存当前库 → 换回备份文件 → 用**同一主密钥**启动。启动流程会做迁移与加密相关校验。

### PostgreSQL

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
| 原生 DB 备份约定 | 文档约定 |
| `reposentinel doctor` / `backup` / `restore` | 规划中 |
| Prometheus `/metrics` | 规划中 |

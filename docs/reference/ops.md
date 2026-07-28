# 运维手册

面向已跑通 Phase 1 实例的日常操作。完整备份 CLI、指标与发布流水线仍以设计规格为准，下文标注「已实现 / 运维约定 / 未实现」。

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

## 备份与恢复（运维约定）

应用内 `reposentinel backup` / `restore` **尚未实现**。Phase 1 请使用数据库原生工具，并**同时**保管加密主密钥。

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
5. 执行规格中的 `secrets reencrypt`（**命令尚未实现**）后移除 PREVIOUS  

当前 Phase 1 库内加密凭据场景有限，但密钥环逻辑已存在，生产仍应按双密钥纪律操作。

## 升级

1. 备份数据库与主密钥  
2. 阅读 Release / CHANGELOG（正式发布流程规划中）  
3. 替换二进制或镜像  
4. 启动并观察迁移与 `/health/ready`  
5. 烟雾：登录、setup 状态、version API  

数据库版本高于应用支持版本时**拒绝启动**（防误降级）。

## 尚未提供的运维能力

| 能力 | 状态 |
|------|------|
| `reposentinel doctor` | 未实现 |
| `reposentinel backup` / `restore` | 未实现 |
| `reposentinel migrate status` | 未实现（启动时自动迁移） |
| `reposentinel secrets reencrypt` | 未实现 |
| Prometheus `/metrics` | 未实现 |
| 过期 Session 定时清理 Worker | 未实现（改密/重置会主动撤销） |

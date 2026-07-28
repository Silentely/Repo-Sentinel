# 常见问题

## 产品边界

### 为什么登录后没有仓库列表？

当前 **v0.1.0 / Phase 1** 只交付认证与运行时。GitHub App、Webhook 与仓库同步属于后续阶段。仪表盘上的「下一阶段」步骤是有意为之，不是故障。详见 [实现状态](/reference/implementation-status)。

### 可以配置 Telegram 吗？

环境变量里预留了 Telegram / HTTP Webhook 相关键，便于后续接线；**Phase 1 不会发送通知**。

## 安装与启动

### setup 页面提示不允许远程初始化？

默认要求来源 IP 与 Host 均为 loopback。使用域名或反向代理时需临时：

```bash
REPOSENTINEL_SETUP_ALLOW_REMOTE=true
```

完成后关闭。见 [管理员](/guide/administrator)。

### 忘记管理员密码怎么办？

没有邮件找回。在能访问数据库与配置的机器上：

```bash
printf '%s\n' "$NEW_PASSWORD" | .tmp/reposentinel admin reset-password --password-stdin
```

### 主密钥是什么？必须设置吗？

`REPOSENTINEL_ENCRYPTION_KEY` 用于加密库内敏感凭据（后续 Telegram Token、PAT 等）。解码后须为 32 字节。密钥丢失会导致密文无法解密；备份数据库时必须同时备份主密钥。见 [配置参考](/reference/configuration)。

### SQLite 能直接复制 `.db` 文件备份吗？

运行中请用 `sqlite3 ... .backup`。直接拷贝活跃 WAL 库可能不一致。见 [运维手册](/reference/ops)。

## 开发

### `pnpm --dir web test` 报 command not found？

先 `pnpm --dir web install`。

### Go 测试里的 PostgreSQL 失败？

无 Docker/URL 时应跳过或按 `docs/reference/development.md` 启动 test compose，不要把 SQLite 结果当成 Postgres 已验证。

### 文档站怎么预览？

```bash
npm install
npm run docs:dev
```

## 安全

### Session 存在 localStorage 吗？

不。Session 在 HttpOnly Cookie；CSRF 在可读 Cookie + 请求头双提交。

### 为什么改密后其他设备掉线？

设计如此：改密撤销其他 Session；CLI 重置撤销全部 Session。

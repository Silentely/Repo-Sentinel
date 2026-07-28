# 常见问题

## 产品与使用

### 登录后仓库列表是空的？

需要先在 GitHub 安装 App 并配置 Webhook；事件到达后仓库会自动出现。也可在能力支持时登记外部公开仓库。详见 [Docker 部署](/deploy/docker) 与 [快速开始](/guide/quick-start)。

### 为什么新仓库一开始不发 Telegram？

新授权仓库默认处于**基线**状态：只建立当前快照，抑制历史/快照通知洪流。在管理后台确认后点击「完成基线」，之后的变化才会实时通知。

### 如何配置 Telegram？

可在环境变量中设置 `REPOSENTINEL_TELEGRAM_TOKEN` 与 `REPOSENTINEL_TELEGRAM_CHAT_ID`（启动时写入渠道），或在「渠道配置」页保存。Token 使用主密钥加密存储。

## 安装与启动

### setup 提示不允许远程初始化？

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

`REPOSENTINEL_ENCRYPTION_KEY` 用于加密库内敏感凭据（如 Telegram Token）。解码后须为 32 字节。密钥丢失会导致密文无法解密；备份数据库时必须同时备份主密钥。见 [配置参考](/reference/configuration)。

### SQLite 能直接复制 `.db` 文件备份吗？

运行中请用 `sqlite3 ... .backup`。直接拷贝活跃 WAL 库可能不一致。见 [运维手册](/reference/ops)。

## 开发

### `pnpm --dir web test` 报 command not found？

先 `pnpm --dir web install`。

### PostgreSQL 相关测试失败？

无 Docker/URL 时应按 [开发规范](/reference/development) 启动 test compose，不要把 SQLite 结果当成 Postgres 已验证。

### 文档站怎么预览？

```bash
npm install
npm run docs:dev
```

## 安全

### Session 存在 localStorage 吗？

不。Session 在 HttpOnly Cookie；CSRF 在可读 Cookie + 请求头双提交。

### 为什么改密后其他设备掉线？

这是预期行为：改密撤销其他 Session；CLI 重置撤销全部 Session。

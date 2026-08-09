# 常见问题

## 产品与使用

### 登录后仓库列表是空的？

需要先在 GitHub **Install** App（不是只在管理台保存凭据），并保证 Webhook 可达。`installation.created` 事件会写入 Installation 与仓库列表（状态为基线中）。

若日志里已有 `event_type=installation` 且 `status=accepted`，但仪表盘仍无仓库：旧版本只解析了 `repositories_added`，未解析顶层 `repositories`。升级后打开 **GitHub App** 页点 **「从 GitHub 同步仓库」** 即可补拉；新安装事件会自动入库。详见 [Docker 部署 · GitHub App](/deploy/docker#4-github-app创建表单逐项)。

### 创建 GitHub App 时 Callback URL / OAuth 怎么填？

**全部可跳过。** RepoSentinel 不用用户 OAuth：Callback URL 留空；不要勾选 Request user authorization during installation、Device Flow。必填的是 **Webhook Active + URL（`/webhooks/github`）+ Secret**，以及仓库只读权限与事件订阅。逐项说明见 [Docker 部署 · GitHub App](/deploy/docker#4-github-app创建表单逐项) 或管理台「GitHub App」页。

### App ID / 私钥 / Webhook Secret 能在网页里填吗？

**可以。** 管理台 **GitHub App** 页支持填写 App ID、Client ID、Public Base URL、Webhook Secret，以及粘贴私钥 PEM（或填服务器路径）。敏感字段用主密钥加密存库，保存后立即生效。

- 若同一字段已用 `REPOSENTINEL_GITHUB_*` 环境变量设置，页面会显示「环境变量锁定」，不能用网页覆盖（避免和部署配置打架）。  
- `REPOSENTINEL_EXTERNAL_PAT` 仍仅支持环境变量。

### 为什么新仓库一开始不发 Telegram？

新授权仓库默认处于**基线**状态：只建立当前快照，抑制历史/快照通知洪流。对账成功后会**自动结束基线**并开始实时通知；也可在仪表盘点「立即放行」跳过等待。

### 如何配置 Telegram？

可在环境变量中设置 `REPOSENTINEL_TELEGRAM_TOKEN` 与 `REPOSENTINEL_TELEGRAM_CHAT_ID`（启动时写入渠道），或在「渠道配置」页保存。Token 使用主密钥加密存储。

### 收不到通知怎么排查？

按链路逐段确认：

1. 渠道是否**启用**并订阅了对应事件类型（「渠道配置」页勾选；全局功能模块关闭时勾选会灰显）。
2. 事件是否**入库**：仪表盘「最近事件」面板有记录则已入库，可点「打开」核对 GitHub 侧。
3. 投递是否成功：「投递记录」页查看状态；失败有条目展示错误码与中文说明，可单条或批量重试。
4. 事件被**跳过**的原因可查服务端 Debug 日志（`REPOSENTINEL_LOG_LEVEL=debug`），关键词：`webhook ingest skipped`（采集被功能/监控/归档开关拦下）、`notification skipped`（已入库但不在实时通知范围）、`webhook event duplicate skipped`（重复送达去重）。

新仓库先确认是否处于基线抑制（见「为什么新仓库一开始不发 Telegram？」）。

### `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` 和页面里的「签名 Secret」是一回事吗？

**不是。** 两套 Secret 方向相反：

| 配置 | 方向 | 用途 |
|------|------|------|
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | **入站** GitHub → RepoSentinel | 校验 GitHub 推送的 `X-Hub-Signature-256`，必须配置才能收事件 |
| 渠道页「HTTP Webhook · 签名 Secret」/ `REPOSENTINEL_HTTP_WEBHOOK_SECRET` | **出站** RepoSentinel → 你的 URL | 可选；配置后在请求头带 `X-GitHub-Monitor-Signature-256` |

HTTP 出站 Secret 用环境变量**或**管理台二选一即可：环境变量只在该渠道尚不存在时种子写入数据库，之后以页面保存为准，不必两边都填。

### 日志太多怎么办？

默认 `info` 下**不再**打印每条页面/API 访问记录（已降为 `debug`）。`info` 保留登录成功/失败、Webhook 受理与处理、通知投递等重点事件。需要访问明细时设 `REPOSENTINEL_LOG_LEVEL=debug`。

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

### 怎么选 SQLite 还是 PostgreSQL？

- **默认 SQLite**：单机 / 单容器最简单，Compose 已配置 `file:/data/reposentinel.db` 与数据卷  
- **PostgreSQL**：设 `REPOSENTINEL_DATABASE_DRIVER=postgres` 与  
  `REPOSENTINEL_DATABASE_URL=postgres://用户:密码@主机:5432/库名?sslmode=require`  
  库需事先创建；应用启动自动迁移  

示例与完整变量见 [配置参考](/reference/configuration)、[Docker 部署](/deploy/docker)。

### 启动报 `database_unavailable` / 无法打开数据库？

常见原因：

1. **密码特殊字符未 URL 编码**（最常见）：密码含 `#` `@` `(` 等时，连接串会被截断。把密码编码后再拼进 URL（`#`→`%23`，`(`→`%28`）。见 [配置参考](/reference/configuration)。  
2. **驱动仍是 sqlite**：确认 `REPOSENTINEL_DATABASE_DRIVER=postgres`（不要写成 `postgresql`）。  
3. **主机/网络**：PaaS 内网主机名是否与官方连接信息一致；应用与数据库是否同一私有网络。  
4. **库不存在 / 认证失败 / SSL**：库名、用户密码、`sslmode`（内网常 `disable`，公网 `require`）。  

新版本日志会在 `message=` 后附带**不含密码**的原因摘要（如「连接串含未编码的 #」）。

### 启动报 `migration_failed`？

- 优先使用**空业务库**；Neon / Nile 等托管库可有默认 schema，当前版本已支持 dirty 首次迁移。  
- 连接用户必须对目标 schema（默认 `public`）有 **CREATE** 权限。Aiven 等只读/受限账号会报 `permission denied for schema public`——在控制台用管理员执行：  
  `GRANT CREATE ON SCHEMA public TO 你的用户;`  
  或改用可建表的角色（如 Aiven 的 `avnadmin`）。  
- 若库里已有**无关业务表**且无本项目的 `atlas_schema_revisions`，迁移会在 `public` 建表，可能冲突——请新建专用库。  
- Nile 等禁止 `set_config(search_path)` 的平台需使用含兼容修复的版本。  
- 若 revision 高于当前二进制，需升级程序，不能用旧版本连新库。

### 环境变量一览在哪？

仓库根目录 [`.env.example`](https://github.com/Silentely/Repo-Sentinel/blob/main/.env.example)，说明见 [配置参考](/reference/configuration)。

### SQLite 能直接复制 `.db` 文件备份吗？

运行中请用应用 `backup` 命令或 `sqlite3 ... .backup`。直接拷贝活跃 WAL 库可能不一致。见 [运维手册](/reference/ops)。

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

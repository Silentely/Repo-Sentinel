# 管理员与 Session

RepoSentinel 只允许**一个**管理员账号。不提供注册、忘记密码邮件、OIDC 或 RBAC。

## 首次创建

### 环境变量引导

同时设置用户名和密码后再启动。密码建议用 `read -s` 注入，避免进入 shell 历史：

```bash
read -r -s REPOSENTINEL_ADMIN_PASSWORD
export REPOSENTINEL_ADMIN_PASSWORD
REPOSENTINEL_ADMIN_USERNAME="Repo Admin" .tmp/reposentinel serve
unset REPOSENTINEL_ADMIN_PASSWORD
```

只提供其中一项会得到 `validation_failed`。创建成功后可移除这两个变量；重启不会覆盖已有密码。

### Setup 页面

无管理员时访问首页会进入「创建唯一管理员」。

**默认安全边界**：setup 要求连接来源 IP **与** 请求 Host 都是 loopback（`127.0.0.1`、`::1`、`localhost`）。即便反向代理与应用同机，经公网域名访问仍会被拒绝。

公网初始化必须显式：

```bash
REPOSENTINEL_SETUP_ALLOW_REMOTE=true
```

并确保 TLS、反向代理访问控制已就绪；完成后应立刻改回 `false`。

成功 setup 会立即建立 Session。管理员已存在时 `GET /api/v1/setup/status` 返回 `required=false`，再次 `POST /api/v1/setup` 返回 `not_found`。

## 登录与 Cookie

| 接口 | 说明 |
|------|------|
| `POST /api/v1/auth/login` | 登录 |
| `GET /api/v1/auth/session` | 当前 Session（需登录） |
| `POST /api/v1/auth/logout` | 退出（需 CSRF） |
| `POST /api/v1/auth/password` | 修改密码（需 CSRF） |

行为要点：

- Session 原始令牌仅通过 HttpOnly Cookie `reposentinel_session` 下发；库内只存哈希
- 写请求需要非 HttpOnly Cookie `reposentinel_csrf` 与请求头 `X-CSRF-Token` 一致
- 默认 Session TTL 24h，可用 `REPOSENTINEL_ADMIN_SESSION_TTL` 调整
- 登录限流按来源 IP 在**进程内存**生效；多副本需在入口层额外限流
- HTTPS 的 `PublicBaseURL` 会使 Cookie 带 `Secure`

UI 改密会保留当前 Session、撤销其他 Session。

## CLI 重置密码

密码不能作为 argv 传入，必须 stdin：

```bash
read -r -s ADMIN_PASSWORD
printf '%s\n' "$ADMIN_PASSWORD" | .tmp/reposentinel admin reset-password --password-stdin
unset ADMIN_PASSWORD
```

成功输出只说明已重置并撤销全部旧 Session。若使用 YAML 配置：

```bash
printf '%s\n' "$ADMIN_PASSWORD" | \
  .tmp/reposentinel admin reset-password --config configs/reposentinel.example.yaml --password-stdin
```

登录页可链到文档说明 CLI 重置，**不会**提供邮件找回。

## 常见错误码

| error_code | 含义 |
|------------|------|
| `invalid_credentials` | 用户名或密码错误 |
| `rate_limited` | 登录过于频繁 |
| `csrf_failed` | 写请求缺少或错误 CSRF |
| `unauthorized` | 未登录或 Session 失效 |
| `validation_failed` | 参数不合法（如密码过短） |
| `not_found` | setup 在已初始化后再次提交等 |

更多配置见 [配置参考](/reference/configuration)。

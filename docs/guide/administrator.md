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

成功设置后立即建立 Session。管理员已存在时 `GET /api/v1/setup/status` 返回 `required=false`，再次 `POST /api/v1/setup` 返回 `not_found`。

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

## 受信任反向代理与 IP 解析

当 RepoSentinel 部署在 Nginx、Caddy、Cloudflare 或 Kubernetes Ingress 等反向代理之后时，默认出于安全考虑会忽略客户端伪造的 `X-Forwarded-For` 请求头，使用直连 socket IP。

若需正确获取客户端真实 IP 进行限流与审计，请在配置文件或环境变量中指定受信任的代理地址或子网：

- **配置文件**（`configs/reposentinel.yaml`）：
  ```yaml
  http:
    trusted_proxies:
      - "127.0.0.1"      # 本机反代
      - "10.0.0.5"       # 反代容器/虚拟机实际地址
  ```
  > **只列真实反代地址，不要把整段私网视为可信。** `trusted_proxies` 决定 `X-Forwarded-For` / `X-Real-IP` 何时被采信，而这个客户端地址同时是首启 setup 环回门、登录限流分桶、2FA 票据 IP 绑定、会话与审计 IP 的唯一依据。列入 `10.0.0.0/8` 这类整段后，位于该网段内且能直连应用端口的任意主机都可以用 `X-Forwarded-For: 127.0.0.1` 通过 setup 环回门、自选限流分桶身份，或让 2FA 票据的 IP 绑定失效。
- **环境变量**（逗号分隔）：
  ```bash
  REPOSENTINEL_HTTP_TRUSTED_PROXIES="127.0.0.1,10.0.0.5"
  ```

只有当直连来源本身属于受信任子网时，服务端才会从右向左回溯 `X-Forwarded-For`，剥离受信任代理，取第一个不属于受信任子网的地址作为客户端真实 IP；`X-Real-IP` 作为回退。若 `X-Forwarded-For` 全部条目均为受信任地址（没有非受信任地址可取），回退直连对端地址而非最左段——后者会让受信任网段内的主机自选客户端身份。

TOTP 密钥由系统主密钥（`cryptox.KeyRing`）采用 AES-GCM 信封加密存储；未配置主密钥时开启 2FA 会被拒绝（503 `encryption_unavailable`）而不是降级为明文存储。

## 两步验证 (2FA / TOTP)

RepoSentinel 支持基于 RFC 6238 标准的时间同步动态口令（TOTP）二步验证。

### 1. 开启两步验证
1. 登录管理后台，进入「设置」页面；
2. 找到「两步验证 (2FA / TOTP)」设置卡片，点击「配置并开启两步验证」；
3. 使用 Google Authenticator、1Password、Bitwarden 等验证器应用扫描或手动输入密钥，或点击「应用快速唤起绑定」；
4. 输入验证器当前显示的 6 位动态验证码并确认，即可成功激活。

激活后，TOTP 密钥将由系统主密钥（`cryptox.KeyRing`）采用 AES-GCM 信封加密存储。

> **升级注意**：0.6.1 之前的版本在未配置主密钥时会把 TOTP 种子以明文（`plain_secret`）落库。
> 当前版本只接受信封加密的种子：若库内存在这类历史明文行，登录会返回
> `503 totp_config_unreadable`（而不是「用户名或密码错误」）。恢复方式二选一：
> 补齐主密钥后重新开启两步验证，或在服务器上执行 `reposentinel admin reset-2fa`
> 解除后重新绑定。

### 2. 两阶段登录体验
开启两步验证后，登录页采用两阶段流程：
1. 第一阶段提交用户名与密码，若验证通过且账号启用了 2FA，服务端签发具备 3 分钟有效期的临时一次性票据（Ticket）；
2. 登录页自动平滑切换至动态口令输入界面，输入 6 位验证码后完成正式认证并下发安全 Session Cookie。

### 3. CLI 应急重置两步验证 (防锁死)
若因更换手机、丢失验证器导致无法登录，管理员可直接通过服务器命令行应急解除 2FA，无需重置密码：

```bash
.tmp/reposentinel admin reset-2fa
```
若使用非默认配置文件路径：
```bash
.tmp/reposentinel admin reset-2fa --config configs/reposentinel.yaml
```
执行后将输出 `reset=ok 2fa_disabled=true`，两步验证立即解除，随后可直接使用原本的账号密码登录。

## CLI 重置密码

密码不能作为命令行参数传入，必须通过标准输入（stdin）提供：

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

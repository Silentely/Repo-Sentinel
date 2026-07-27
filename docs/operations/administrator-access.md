# 管理员访问 RepoSentinel

RepoSentinel 只允许一个管理员账号。首次启动可以用环境变量引导管理员，也可以通过本机 setup 页面创建；已有管理员时，后续启动不会用环境变量覆盖密码。

## 首次创建唯一管理员

### 通过环境变量引导

同时设置用户名和密码，再启动一次服务。下面的命令从终端读取密码，不把它放进命令历史：

```bash
read -r -s REPOSENTINEL_ADMIN_PASSWORD
export REPOSENTINEL_ADMIN_PASSWORD
REPOSENTINEL_ADMIN_USERNAME="Repo Admin" ".tmp/reposentinel" serve
unset REPOSENTINEL_ADMIN_PASSWORD
```

两项只出现一项会导致 `validation_failed`。管理员创建成功后，可以移除这两个环境变量；重新启动不会覆盖已有密码。

### 通过 setup 页面创建

没有管理员时，访问实例首页会跳转到“创建唯一管理员”。默认情况下，setup 只接受连接来源 IP 与请求 Host 都是 loopback 的直连请求，例如 `http://127.0.0.1:8080`、`http://[::1]:8080` 或 `http://localhost:8080`。即使反向代理与 RepoSentinel 位于同一台主机，通过公网域名访问仍会被拒绝；此时必须显式设置 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`。远程 setup 只应在 TLS、反向代理认证和网络白名单都已就绪后启用，完成后应立即恢复为 `false`。

成功 setup 会立即建立 Session。管理员已存在时，setup 状态变为 `required=false`，再次提交返回 `not_found`。

## 登录和 Session 行为

- 登录接口：`POST /api/v1/auth/login`。
- 当前 Session：`GET /api/v1/auth/session`。
- 退出接口：`POST /api/v1/auth/logout`。
- Session 默认有效期为 24 小时，可用 `REPOSENTINEL_ADMIN_SESSION_TTL` 调整。
- Session 原始令牌只通过 HttpOnly Cookie `reposentinel_session` 返回；服务端只保存哈希。
- 写请求还需要非 HttpOnly Cookie `reposentinel_csrf` 对应的 `X-CSRF-Token` 请求头。
- 登录限流按来源 IP 在进程内生效，不因更换用户名获得新额度；多实例部署仍需要在入口层增加共享限流或粘性策略。

退出会撤销当前 Session 并清理认证 Cookie。密码修改会保留当前 Session、撤销其他 Session；CLI 重置会撤销全部旧 Session。

## 使用 CLI 重置密码

密码不能作为命令行参数传入。使用 `--password-stdin`，程序只读取 stdin 第一行，并且不会把密码写入输出：

```bash
read -r -s ADMIN_PASSWORD
printf '%s\n' "$ADMIN_PASSWORD" | ".tmp/reposentinel" admin reset-password --password-stdin
unset ADMIN_PASSWORD
```

成功输出只会说明密码已重置及旧 Session 已撤销。非交互脚本也必须通过 stdin 管道提供密码；把密码写进 shell history、进程参数或 CI 日志会绕过这条安全边界。

如果实例使用非默认 YAML 配置，给恢复命令同样传入 `--config`：

```bash
read -r -s ADMIN_PASSWORD
printf '%s\n' "$ADMIN_PASSWORD" | \
  ".tmp/reposentinel" admin reset-password --config ".tmp/reposentinel.yaml" --password-stdin
unset ADMIN_PASSWORD
```

## 识别常见错误

| 错误码 | 处理建议 |
| --- | --- |
| `validation_failed` | 检查配置字段、管理员用户名/密码是否成对，以及密码输入是否为空。 |
| `encryption_key_mismatch` | 检查当前/上一把主密钥与恢复的数据库是否匹配。不要删除数据库来绕过校验。 |
| `database_unavailable` | 检查数据库 URL、迁移状态、文件权限或 PostgreSQL 网络连接。 |
| `invalid_credentials` | 重新输入凭据；遗失密码时使用本机 CLI 重置。 |
| `unauthorized` | Session 已过期、被撤销或 Cookie 未发送，需要重新登录。 |
| `csrf_failed` | 确认浏览器保留了 `reposentinel_csrf` Cookie，并在写请求发送 `X-CSRF-Token`。 |

错误响应只包含稳定的 `error_code` 和安全说明，不会返回数据库连接串、密码、主密钥或底层堆栈。

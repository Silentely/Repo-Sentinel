# auth

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **auth**

## 模块职责

单管理员认证域：Argon2id 密码哈希、Session 签发/校验/清理、双提交 CSRF、登录限流与管理员服务（创建/改密）。

## 入口与启动

| 类型 | 说明 |
|------|------|
| `AdminService` | 创建管理员、校验密码、改密 |
| `SessionService` | 创建/查找/撤销/清理过期 Session |
| `CSRFTokens` | 生成与校验 CSRF |
| `LoginLimiter` | 按身份限流 |
| `PasswordHasher` | Argon2id |

由 `app.Build` 构造并注入 `httpapi`。

## 对外接口

无 HTTP；由 `httpapi` auth/setup handlers 调用。  
错误通过包内 coded error 映射为稳定 error_code。

## 关键依赖与配置

- `store.AdminStore` / `SessionStore` / `AuditStore`
- Session TTL 来自 `config.Admin.SessionTTL`
- Cookie 策略（Secure）由 httpapi 根据 PublicBaseURL 决定

## 数据模型

- `store.AdminAccount`、`store.AdminSession`（TokenHash / CSRFHash 存库，原始令牌仅 Cookie）

## 测试与质量

- `admin_service_test.go`、`session_service_test.go`、`csrf_test.go`、`password_test.go`、`login_limiter_test.go`

## 常见问题 (FAQ)

**Q: 多管理员？**  
A: 产品模型为唯一管理员；`GetOnly` / setup 流程按此设计。

**Q: 登出如何失效？**  
A: Session 行删除（Revoke），非仅清 Cookie。

## 相关文件清单

- `admin_service.go`、`session_service.go`、`csrf.go`、`password.go`、`login_limiter.go`、`errors.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

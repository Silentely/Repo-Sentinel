# cryptox

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **cryptox**

## 模块职责

凭据与敏感字段加密域：AES-256-GCM 信封加密（rs1）、双密钥平滑轮换支持、HMAC 密钥派生。

## 入口与启动

| 类型 / 函数 | 说明 |
|------|------|
| `KeyRing` | 双密钥环，提供 `Encrypt`、`Decrypt` 与 `DeriveHMACKey` |
| `NewKeyRing` | 解析配置并初始化主密钥与上一把密钥 |

由 `app.Build` 构造并注入 `httpapi` 与后台服务。

## 对外接口

无 HTTP；供 `httpapi`、`app`、`notify` 消费。

## 关键依赖与配置

- `config.EncryptionConfig`（CurrentKey / PreviousKey）
- `crypto/aes`、`crypto/cipher`、`crypto/rand`

## 数据模型

- 信封格式：`rs1.<key_id>.<base64url_gcm_payload>`

## 测试与质量

- `keyring_test.go`：包含信封加密往返、密钥轮换、非法信封与基准测试。

## 变更记录 (Changelog)

| 日期 | 版本 / 范围 | 说明 |
|------|------------|------|
| 2026-09-25 | 性能优化 | 优化信封解密严格校验与内存分配开销 |
| 2026-08-05 | 模块初始化 | 初始化模块 AI 上下文文档 |

> 完整历史变更请查阅根目录 [`CHANGELOG.md`](../../CHANGELOG.md)。

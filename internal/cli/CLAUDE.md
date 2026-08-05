# cli

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **cli**

## 模块职责

命令行分派层：解析子命令、加载配置、调用 `app` 或本机运维能力，统一以 `error_code=` / `message=` 向 stderr 输出安全错误。

## 入口与启动

- `cli.Run(args, stdout, stderr)` — 生产入口（绑定 SIGINT/SIGTERM）
- `NewRunner(...).Run(ctx, args)` — 可注入依赖的测试入口

## 对外接口（子命令）

| 命令 | 说明 |
|------|------|
| `serve [--config path]` | 加载配置 → `app.Build` → `Run` |
| `version` | 打印构建信息 |
| `config validate` | 仅校验配置 |
| `admin reset-password` | 重置唯一管理员密码 |
| `doctor` | 本机诊断 |
| `healthcheck` | 探测 live/ready |
| `backup` | SQLite/Postgres 备份 |
| `restore` | 从备份恢复 |

未知命令或参数错误返回 `validation_failed`。

## 关键依赖与配置

- `config.Load` / `config.LoadOptions`
- `app.Build`、`app.ResetAdminPassword`
- `buildinfo.Current`

`Dependencies` 结构体允许替换上述函数，便于单测。

## 数据模型

无独立模型；backup/restore 直接操作配置中的数据库。

## 测试与质量

- `cli_test.go`、`run_test.go`、`backup_test.go`、`healthcheck_test.go`
- 断言错误码与公开文案，避免泄露内部 cause

## 常见问题 (FAQ)

**Q: serve 失败时 exit code？**  
A: `main` 在 `cli.Run` 返回非 nil 时 `os.Exit(1)`；stderr 含稳定 error_code。

**Q: backup 支持哪些驱动？**  
A: `sqlite` 与 `postgres`（见 `backup.go`）。

## 相关文件清单

- `cli.go`、`run.go`、`version.go`、`config_validate.go`
- `admin_reset_password.go`、`doctor.go`、`healthcheck.go`
- `backup.go`（含 restore）

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

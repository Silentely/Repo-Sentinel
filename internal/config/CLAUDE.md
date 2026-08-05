# config

[根目录](../../CLAUDE.md) > [internal](../CLAUDE.md) > **config**

## 模块职责

加载 YAML 配置文件、合并环境变量默认值、校验必填项，并用 `Secret` 类型避免密钥被日志/JSON 意外泄露。

## 入口与启动

- `config.Load(ctx, LoadOptions)` — 主入口
- `defaults.go` — 默认值（HTTP、DB、聚合窗口、AI 等）
- `Validate()` — 结构校验

`LoadOptions` 可注入 `FileSystem` 与 `LookupEnv`，便于测试。

## 对外接口

顶层 `Config` 字段（`types.go`）：

| 段 | 用途 |
|----|------|
| `http` | 监听地址、PublicBaseURL |
| `database` | driver=`sqlite`\|`postgres`、URL、连接池 |
| `admin` | 引导用户名/密码、SessionTTL |
| `setup` | `allow_remote` 首次设置策略 |
| `encryption` | 当前/上一把主密钥 |
| `github` | AppID、私钥路径、Webhook Secret、External PAT |
| `notify` | Telegram / HTTP Webhook |
| `logging` | format、level |
| `metrics` | 是否启用、Bearer Token |
| `update_check` | 远程版本检查 |
| `aggregation` | 通知合并窗口与超频阈值 |
| `ai` | OpenAI 兼容端点、模型、摘要/分诊开关 |

Secret 典型环境变量（示例配置注释为准）：

- `REPOSENTINEL_ENCRYPTION_KEY` / `_PREVIOUS`
- `REPOSENTINEL_ADMIN_PASSWORD`
- `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` / `_PREVIOUS`
- `REPOSENTINEL_EXTERNAL_PAT`
- `REPOSENTINEL_TELEGRAM_TOKEN`
- `REPOSENTINEL_HTTP_WEBHOOK_SECRET`
- `REPOSENTINEL_AI_API_KEY`
- `REPOSENTINEL_METRICS_TOKEN`
- `REPOSENTINEL_UPDATE_CHECK_TOKEN`
- `REPOSENTINEL_DATABASE_URL`

## 关键依赖与配置

- 示例文件：`configs/reposentinel.example.yaml`
- 文档：`docs/reference/configuration.md`、`docs/operations/configuration.md`

## 数据模型

无持久化；`Secret` 为内存包装类型（`secret.go`）。

## 测试与质量

- `load_test.go`、`validate_test.go`
- 校验失败应返回明确错误，禁止半初始化 Config 进入 `app.Build`

## 常见问题 (FAQ)

**Q: 密钥能写进 YAML 吗？**  
A: 设计上 Secret 由环境变量注入；示例文件刻意不写明文密钥。

**Q: PublicBaseURL 有何影响？**  
A: 用于 Secure Cookie 判定（https → Secure）及对外链接语义。

## 相关文件清单

- `types.go`、`defaults.go`、`load.go`、`validate.go`、`secret.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

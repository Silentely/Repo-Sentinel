# cmd

[根目录](../CLAUDE.md) > **cmd**

## 模块职责

Go 进程入口目录。当前仅含 `reposentinel` 单命令，将 `os.Args` 交给 `internal/cli` 分派，不包含业务逻辑。

## 入口与启动

| 文件 | 说明 |
|------|------|
| `reposentinel/main.go` | `cli.Run(os.Args[1:], os.Stdout, os.Stderr)`；非 nil 错误时 `os.Exit(1)` |

本地构建：

```bash
make build                 # 输出 ./reposentinel
make build-production      # -tags production，嵌入 web/dist
./reposentinel serve --config configs/reposentinel.example.yaml
```

## 对外接口

无独立 API；子命令见 [internal/cli/CLAUDE.md](../internal/cli/CLAUDE.md)。

## 关键依赖与配置

- 依赖：`github.com/Silentely/Repo-Sentinel/internal/cli`
- 构建 ldflags 由根 `Makefile` 注入 `internal/buildinfo`

## 数据模型

无。

## 测试与质量

- 入口极薄，行为测试集中在 `internal/cli`、`internal/app`
- `make build` / `go build ./cmd/reposentinel` 作为冒烟

## 常见问题 (FAQ)

**Q: 为什么 main 只有几行？**  
A: 便于 CLI 在测试中注入 stdin/stdout 与依赖，避免 main 包难测。

**Q: production tag 有什么用？**  
A: `web/embed_production.go` 在 production 下嵌入 `web/dist`；开发 tag 使用 fallback。

## 相关文件清单

- `reposentinel/main.go`

## 变更记录 (Changelog)

| 时间戳 (UTC) | 变更摘要 |
|---|---|
| 2026-08-05T09:57:59Z | 初始化模块 AI 上下文文档 |

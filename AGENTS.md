# AGENTS.md

本文件为 AI Agent（包括 Claude Code、Codex、Cursor、Antigravity 等）在参与本仓库开发时的统一行动指南。项目整体架构、模块约定与开发规范遵循 [`CLAUDE.md`](./CLAUDE.md)。

## 提交前代码格式化规范（强制）

- **Go 代码格式化**：凡涉及 Go 代码的修改（位于 `cmd/`、`internal/`、`migrations/` 等目录），在执行 `git commit` 之前，**必须**对修改涉及的文件执行格式化：
  ```bash
  gofmt -w <修改的.go文件>
  # 或全量执行
  gofmt -w cmd internal migrations
  ```
- **门禁断言**：提交前必须运行以下命令，确认输出为空：
  ```bash
  gofmt -l cmd internal migrations
  ```
  CI 流水线（`.github/workflows/docker.yml` 的 `Gofmt check`）对未格式化文件实行零容忍拦截，任何未格式化的 Go 代码都将直接导致 Docker 镜像构建失败。

## 提交规范

- 提交信息格式遵循 `<emoji> <type>: <中文描述>`，详见 [`CLAUDE.md`](./CLAUDE.md)。
- 严禁在提交信息中加入 `Co-Authored-By: Claude` 等 AI 生成标识。
- 一个提交专注解决一个问题，不做无关改动。

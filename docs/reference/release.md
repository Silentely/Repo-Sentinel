# 发布与镜像

本文面向**维护者**。部署用户请用 [Docker 部署](/deploy/docker)，直接拉：

```text
ghcr.io/silentely/repo-sentinel:latest
```

权威检查清单见 [`.github/RELEASE_RULES.md`](https://github.com/Silentely/Repo-Sentinel/blob/main/.github/RELEASE_RULES.md)。

## 镜像标签（摘要）

| 触发 | 标签 | 说明 |
|------|------|------|
| `main` 推送 | `main`、`main-<sha>` | 主干滚动（amd64） |
| `dev` 推送 | `dev`、`dev-<sha>` | 开发 / 预发（amd64） |
| Git tag `v*` | `vX.Y.Z`、`latest` | 正式版（amd64 + arm64） |

- 生产优先 `latest` 或钉死 `vX.Y.Z`
- 正式 tag 双架构 + QEMU，通常比 `main` 单架构更久
- 工作流：[`.github/workflows/docker.yml`](https://github.com/Silentely/Repo-Sentinel/blob/main/.github/workflows/docker.yml)

## 版本约定

| 项 | 约定 |
|----|------|
| 唯一真实来源 | 根目录 [`VERSION`](https://github.com/Silentely/Repo-Sentinel/blob/main/VERSION)（SemVer，无 `v`） |
| Git tag | `v` + `VERSION`，推送后触发 Docker CI |
| 变更日志 | [`CHANGELOG.md`](https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md) |

## 发版步骤（摘要）

1. `main` 已含待发布提交，CI 绿  
2. 改 `VERSION`，更新 `CHANGELOG.md`（及 README 徽章等展示）  
3. 推 `main` → 打 **新** tag `vX.Y.Z` 并 push（禁止覆盖已发布 tag）  
4. 等 Docker 工作流成功；创建 GitHub Release  
5. 验证 `docker pull ghcr.io/silentely/repo-sentinel:vX.Y.Z` 与 `latest`  
6. 将 `main` 同步到 `dev`  

本地门禁：`go test` + `go vet` + `pnpm --dir web typecheck` + `make build`（或 production 构建）。

## 禁止

- 跳过测试发布、CI 未绿就宣称完成  
- 覆盖已发布 `v*` tag  
- commit 含 `Claude` / 生成器署名  

## 回滚

不删已发 tag；部署改回上一 `v*` 镜像；有问题发新 patch。详见 RELEASE_RULES。

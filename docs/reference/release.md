# 发布与镜像标签

本文说明维护者如何发版，以及 GHCR 镜像标签规则。部署用户请直接使用 [Docker 部署](/deploy/docker)。

## 镜像仓库

```text
ghcr.io/silentely/repo-sentinel
```

生产推荐：

```bash
docker pull ghcr.io/silentely/repo-sentinel:latest
# 或钉死版本
docker pull ghcr.io/silentely/repo-sentinel:v0.3.4
```

## 标签规则

| 触发 | 推送的标签 | 架构 | 典型耗时 |
|------|------------|------|----------|
| 推送到 `main` | `main`、`main-<12 位 sha>` | linux/amd64 | 约 3–8 分钟（有缓存更短） |
| 推送到 `dev` | `dev`、`dev-<12 位 sha>` | linux/amd64 | 约 3–8 分钟 |
| 推送 Git 标签 `v*` | `vX.Y.Z`、`latest` | linux/amd64 + linux/arm64 | 约 15–30 分钟（QEMU 交叉构建） |
| 手动 `workflow_dispatch` | 按当前分支 / tag 走上表规则 | 同上 | 同上 |

说明：

- **`latest` 仅在正式 `v*` 发版时更新**，不会随 `main` 每次提交滚动。
- **`main` 浮动标签**表示主干最新构建，适合预发或跟主干；生产更建议 `latest` 或钉死 `v*`。
- 工作流文件：[`.github/workflows/docker.yml`](https://github.com/Silentely/Repo-Sentinel/blob/main/.github/workflows/docker.yml)。

## 为何正式发版比 main 慢？

| 阶段 | main / dev | 正式 `v*` |
|------|------------|-----------|
| 测试 | `go test` + 前端 | 相同 |
| 镜像平台 | 仅 `amd64` | `amd64` + `arm64` |
| QEMU | 否 | 是（模拟 arm64，显著变慢） |

因此：**main 推送约 5 分钟量级是正常的**；**带 `latest` 的正式 tag 构建更久是预期现象**，不是卡死。可在 Actions 的 `build-and-push` 步骤查看进度。

## 维护者发版清单

1. 确认 `main` 已包含待发布提交，且 CI 测试通过  
2. 更新根目录 `VERSION`（SemVer，无 `v` 前缀）  
3. 更新 `CHANGELOG.md` 对应章节  
4. 同步 README / 文档中的版本徽章或示例标签（如有）  
5. 合并并推送到 `main`（会产出 `main` / `main-<sha>` 镜像）  
6. 打 tag 并推送：

```bash
git checkout main
git pull
git tag -a v0.3.4 -m "RepoSentinel v0.3.4"
git push origin v0.3.4
```

7. 创建 GitHub Release（可与 tag 同步），正文引用 CHANGELOG  
8. 等待 Actions 中 **Docker Image** 对 `v*` 的 run 成功  
9. 验证：

```bash
docker pull ghcr.io/silentely/repo-sentinel:v0.3.4
docker pull ghcr.io/silentely/repo-sentinel:latest
docker run --rm ghcr.io/silentely/repo-sentinel:latest version
```

## 版本号约定

- 真相源：仓库根目录 `VERSION`  
- Git tag：`v` + `VERSION`（如 `0.3.4` → `v0.3.4`）  
- 二进制 / 镜像内版本：构建时 ldflags 注入  
- 变更日志：[CHANGELOG.md](https://github.com/Silentely/Repo-Sentinel/blob/main/CHANGELOG.md)（Keep a Changelog）

## 回滚

- 部署侧将 Compose / run 的 image 改回上一 `v*` 标签并 `compose pull && up -d`  
- 数据库若已跑新迁移，降级前确认迁移兼容性；更高 schema 版本会拒绝被旧二进制启动  
- 加密主密钥必须与备份库匹配  

## 相关链接

- [贡献指南](https://github.com/Silentely/Repo-Sentinel/blob/main/CONTRIBUTING.md)  
- [安全策略](https://github.com/Silentely/Repo-Sentinel/blob/main/SECURITY.md)  
- [Docker 部署](/deploy/docker)  

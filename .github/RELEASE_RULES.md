# RepoSentinel 发布规则

> 本文档定义维护者 / Agent 在执行版本发布时必须遵循的规则与流程。  
> 镜像标签与运维说明见 [docs/reference/release.md](../docs/reference/release.md)。

## 版本号规范

采用 [语义化版本 (SemVer)](https://semver.org/lang/zh-CN/)：`MAJOR.MINOR.PATCH`

| 类型 | 升级位 | 触发条件 | 示例 |
|------|--------|----------|------|
| **主版本** (MAJOR) | **X**.0.0 | 破坏性变更、不向后兼容的 API / 配置 / 数据迁移 | 0.3.4 → 1.0.0 |
| **次版本** (MINOR) | 0.**Y**.0 | 新功能、新模块、显著改进（向后兼容） | 0.3.4 → 0.4.0 |
| **补丁版本** (PATCH) | 0.0.**Z** | Bug 修复、文档修正、CI / 依赖调整 | 0.3.4 → 0.3.5 |

### 预发布标识（暂不使用）

项目当前不使用 `-alpha` / `-beta` / `-rc` 后缀。如需引入，须在本文件中新增规则。

## 版本号唯一真实来源

| 角色 | 位置 | 说明 |
|------|------|------|
| **唯一真实来源** | 仓库根目录 `VERSION` | 纯 SemVer 文本，**无** `v` 前缀（例如 `0.3.5`） |
| Git tag | `v` + `VERSION` | 例如 `0.3.5` → `v0.3.5`；推送后触发 Docker CI |
| 二进制 / 镜像内版本 | ldflags / Docker `VERSION` build-arg | 由 `Makefile` 或 CI 从 `VERSION` / tag 注入 |
| CHANGELOG | `CHANGELOG.md` | Keep a Changelog；发版时新增对应章节 |
| 展示用副本 | README 徽章、`docs/` 示例标签等 | **派生展示**，发版时与 `VERSION` 对齐，不得单独漂移 |

### 禁止多源手写产品版本

以下**不得**作为第二真相源（发版时若出现硬编码产品版本，必须与 `VERSION` 一致或改为引用说明）：

- `Dockerfile` 中 `ARG VERSION=…` 仅作本地默认值，正式构建由 CI 覆盖
- `web/package.json` 的 `version` 为前端私有包字段，**不是**产品版本
- 根目录 `package.json` 为文档站工具包，**不是**产品版本

对照邻项目 TG-SignPulse：`tg_signer/__init__.py` 的 `__version__` 为其唯一真实来源；本仓库对应物即为 **`VERSION` 文件**。

## 发布前置条件

在执行发布操作前，**必须确认**以下所有条件均满足。

### 代码质量（四项全过）

对应邻项目 `pytest + ruff + typecheck + build`，本仓库门禁为：

| 门禁 | 命令 | 说明 |
|------|------|------|
| 后端测试 | `go test ./...` | 等价 pytest |
| 静态检查 | `go vet ./...`（建议同时 `go fmt ./...`） | 等价 ruff |
| 前端类型 | `pnpm --dir web typecheck` | typecheck |
| 构建 | `make build` 或 `pnpm --dir web build` + `make build-production` | build |

一键本地验证（推荐）：

```bash
go test ./...
go vet ./...
pnpm --dir web typecheck
pnpm --dir web test --run
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
.tmp/reposentinel version   # 核对注入的版本元数据
```

CI（`.github/workflows/docker.yml` 的 `test` job）在构建镜像前至少执行：`go test`、`pnpm typecheck`、`pnpm test`。  
**四项（或 CI 等价门禁）未全过，禁止打 tag / 宣称发布完成。**

### 变更审查

- [ ] 自上个版本以来的变更已写入 `CHANGELOG.md`
- [ ] 关联 Issue 已关闭或标注已解决
- [ ] 无未完成且影响发布功能的 TODO / FIXME
- [ ] `main` 已包含待发布提交，且相关 CI 为绿色

### 版本号同步检查

发版前逐项确认：

| 位置 | 是否手改 | 说明 |
|------|----------|------|
| `VERSION` | **是（唯一必改）** | 写入新 SemVer |
| `CHANGELOG.md` | 是 | 新增 `[X.Y.Z] - YYYY-MM-DD` 章节 |
| Git tag `vX.Y.Z` | 打 tag 时生成 | 禁止覆盖已存在 tag |
| Docker / GHCR | 否 | 由 tag 推送触发 CI 自动构建 |
| README 版本徽章、`docs/` 中的示例 `vX.Y.Z` | 建议同步 | 展示用，避免文档长期落后 |

## 发布流程

### Step 1：确认版本号

```bash
# 当前版本
tr -d '[:space:]' < VERSION

# 已发布 tag（禁止覆盖）
git tag --list 'v*' | sort -V | tail -5

# 按变更类型决定 MAJOR / MINOR / PATCH
```

### Step 2：更新 `VERSION`

```bash
# 例如发布 0.3.5
printf '0.3.5\n' > VERSION
```

### Step 3：更新 CHANGELOG

在 `CHANGELOG.md` 顶部新增章节，按 Keep a Changelog 分组（`Added` / `Changed` / `Fixed` 等）。  
条目摘要可与 commit 前缀约定一致（见下文）。

### Step 4：同步展示副本（如有）

- README 版本徽章中的 `0.x.y`
- `docs/README.md`、`docs/guide/health-and-version.md`、`docs/deploy/docker.md` 等示例标签
- `Dockerfile` 中 `ARG VERSION` 默认值（可选，便于本地 `docker build` 无 build-arg 时不落后）

### Step 5：提交与打 Tag

```bash
git checkout main
git pull origin main

git add VERSION CHANGELOG.md README.md docs/ Dockerfile   # 按实际改动暂存
git commit -m "chore: 发布版本号同步至 X.Y.Z"

# 注解 tag（必须以 v 开头，且不得已存在）
git tag -a "vX.Y.Z" -m "RepoSentinel vX.Y.Z"

git push origin main
git push origin "vX.Y.Z"
```

### Step 6：验证 CI

推送 `v*` tag 后，**Docker Image** 工作流自动触发：

| 触发 | 推送的 GHCR 标签 | 架构 |
|------|------------------|------|
| `vX.Y.Z` tag | `vX.Y.Z`、`latest` | linux/amd64 + linux/arm64 |
| `main` 推送 | `main`、`main-<12 位 sha>` | linux/amd64 |
| `dev` 推送 | `dev`、`dev-<12 位 sha>` | linux/amd64 |

```bash
gh run list --workflow=docker.yml --limit=5
# 等待 tag 对应 run 成功后再宣称发布完成
```

镜像：`ghcr.io/silentely/repo-sentinel`

### Step 7：GitHub Release 与冒烟

- 创建 GitHub Release（可与 tag 同步），正文引用 `CHANGELOG.md`
- 验证：

```bash
docker pull ghcr.io/silentely/repo-sentinel:vX.Y.Z
docker pull ghcr.io/silentely/repo-sentinel:latest
docker run --rm ghcr.io/silentely/repo-sentinel:latest version
```

## Commit 前缀约定

与仓库现有约定一致；发布相关提交优先使用 `chore:`：

| 前缀 | 含义 |
|------|------|
| `✨ feat:` / `feat:` | 新功能 |
| `🐛 fix:` / `fix:` | Bug 修复 |
| `♻️ refactor:` / `refactor:` | 重构 |
| `🔥 remove:` / `remove:` | 删除废弃代码 |
| `🔧 chore:` / `chore:` | 构建 / CI / 工具链 / 发版同步 |
| `📝 docs:` / `docs:` | 文档 |
| `improve:` | 现有功能改进 |

说明：emoji 可选；含义与类型前缀必须清晰。正文使用祈使句，写清「做了什么 / 为什么」。

## 禁止事项

- 禁止跳过测试 / 门禁直接发布
- 禁止在 CI 未通过时标记发布完成
- 禁止手动覆盖、移动或 force-push **已发布**的 `v*` tag（应发新的 patch）
- 禁止在发布 commit 中混入功能代码
- 禁止 commit / tag 说明中含 `Claude`、`Co-Authored-By: Claude` 等生成器署名字样
- 禁止把 `web/package.json` 或文档站 `package.json` 当作产品版本真相源
- 禁止仅改展示副本却不改 `VERSION`

## 紧急热修复

1. 从最新 release tag 拉分支：`git checkout -b hotfix/vX.Y.Z vX.Y.Z`
2. 仅修目标 Bug，不混入其他变更
3. 跑通四项门禁
4. 升 **PATCH**，按正常流程更新 `VERSION` + CHANGELOG + tag
5. hotfix 合并回 `main`，再同步到 `dev`

## 版本回退

1. **不删除**已发布 tag（GHCR 镜像已存在，删 tag 无法撤回）
2. 立即修复并发布新的 patch
3. 必要时在 GitHub Release 标注问题版本
4. 部署侧将 Compose / run 的 image 改回上一 `v*` 并重新拉取

## 分支与发版关系

| 分支 | 用途 |
|------|------|
| `main` | 稳定主干；正式发版从 `main` 打 `v*` tag |
| `dev` | 开发 / 预发；发版后或合并依赖更新后须与 `main` 同步 |

Dependabot / 功能 PR 合并到 `main` 后，维护者应将 `main` 快进或合并进 `dev`，避免长期分叉。

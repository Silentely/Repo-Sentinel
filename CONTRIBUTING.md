# 贡献指南

感谢关注 RepoSentinel。本文说明如何提 Issue、开 PR，以及本地开发与验证约定。

## 行为准则

- 讨论聚焦问题本身，保持礼貌与可执行反馈
- 不在公开 Issue / PR 中粘贴密钥、Token、Webhook Secret 或完整生产配置
- 安全漏洞请按 [SECURITY.md](SECURITY.md) 私下报告，不要开公开 Issue

## 开发环境

| 组件 | 版本 |
|------|------|
| Go | 见 `go.mod`（当前 1.26+） |
| Node.js | 24+ |
| pnpm | 10.34.5（`web/` 与 `packageManager` 一致） |
| Docker | 可选；生产部署优先使用 GHCR 镜像 |

```bash
# 后端
go test ./...

# 前端
pnpm --dir web install
pnpm --dir web typecheck
pnpm --dir web test --run

# 生产嵌入构建（可选）
pnpm --dir web build
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
```

## 分支与协作

| 分支 | 用途 |
|------|------|
| `main` | 稳定主干；推送后构建 `main` / `main-<sha>` 镜像 |
| `dev` | 开发 / 预发；推送后构建 `dev` / `dev-<sha>` 镜像 |
| 功能分支 | 从 `main` 或 `dev` 拉出，经 PR 合入 |

推荐流程：

1. 开 Issue 描述问题或需求（可用模板）
2. 从最新 `main`（或约定的 `dev`）拉分支
3. 小步提交，提交说明写清「做了什么 / 为什么」
4. 开 PR，填写模板并关联 Issue
5. 通过 CI（`go test`、前端 typecheck / unit）后再合并

## 提交说明

使用类型前缀（emoji 可选），祈使句写清「做了什么 / 为什么」：

| 前缀 | 含义 | 示例 |
|------|------|------|
| `✨ feat:` / `feat:` | 新功能 | `feat: 增加 Prometheus /metrics` |
| `🐛 fix:` / `fix:` | Bug 修复 | `fix: 修复 Outbox 表名映射` |
| `♻️ refactor:` / `refactor:` | 重构 | `refactor: 抽离通知聚合逻辑` |
| `🔥 remove:` / `remove:` | 删除废弃代码 | `remove: 删除未使用的配置项` |
| `🔧 chore:` / `chore:` | 构建 / CI / 依赖 / 发版 | `chore: 发布版本号同步至 0.3.5` |
| `📝 docs:` / `docs:` | 文档 | `docs: 补充 Docker 标签说明` |
| `improve:` | 现有功能改进 | `improve: 缩短对账默认间隔` |

- 避免空泛词（「交付」「对账计划」「Phase」等）
- **禁止** commit 中出现 `Claude`、`Co-Authored-By: Claude` 等生成器署名
- 破坏性变更在正文说明迁移 / 回滚影响

## Pull Request 要求

详见 [.github/PULL_REQUEST_TEMPLATE.md](.github/PULL_REQUEST_TEMPLATE.md)。合并前至少满足：

1. **描述**：动机、改动范围、用户可见行为变化
2. **关联**：`Fixes #n` / `Closes #n`（如有）
3. **验证**：列出本地执行过的命令与结果
4. **CI**：`Docker Image` 工作流中的 test job 通过（路径触发时）
5. **文档**：若变更配置、CLI、镜像标签、API 或部署方式，同步更新 `README` / `docs/` / `CHANGELOG`（按需）
6. **密钥**：不提交 `.env`、私钥、真实 Token

审查关注点：正确性、边界与错误路径、与现有风格一致、不过度扩大范围。

## 发布

- 权威规则：[`.github/RELEASE_RULES.md`](.github/RELEASE_RULES.md)
- 维护者摘要：[docs/reference/release.md](docs/reference/release.md)
- 用户部署文档不要写发版策略表；镜像示例用 `ghcr.io/silentely/repo-sentinel:latest`

产品版本唯一真实来源为根目录 `VERSION`；tag `vX.Y.Z` 触发 Docker CI。  
贡献者保证 `main` 可合并即可；**不要** force-push / 覆盖已发布 `v*` 标签。

## 文档站

用户文档在 `docs/`（VitePress）。本地预览：

```bash
npm install
npm run docs:dev
```

## 需要帮助时

- 使用方式 / 部署：先看 [docs/README.md](docs/README.md) 与 [FAQ](docs/faq.md)
- Bug / 功能：用 Issue 模板
- 安全：SECURITY.md

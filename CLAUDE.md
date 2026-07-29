# CLAUDE.md

## 项目概览

RepoSentinel — 自托管 GitHub 仓库值守平台。Go 后端 + React 前端，Ent ORM + Atlas 迁移，SQLite 默认 / PostgreSQL 可选，Docker 单容器部署。

## 技术栈

| 层 | 技术 |
|---|------|
| 后端 | Go 1.26+，Chi 路由，Ent ORM，Atlas 迁移 |
| 前端 | React 19，TanStack Router/Query，Vite，pnpm 10.34.5，TypeScript |
| 数据库 | SQLite（默认）+ PostgreSQL（可选），双轨迁移 |
| 部署 | Docker / GHCR，CI 由 Tag `v*` 或 main/dev 推送触发 |

## 开发命令

```bash
# 后端
go test ./...
go vet ./...
make build                    # 本地构建
make build-production         # 嵌入前端的生产构建

# 前端
pnpm --dir web install
pnpm --dir web typecheck      # 必须通过
pnpm --dir web test -- --run
pnpm --dir web build

# 综合验证
make verify                   # fmt + test + vet + build
```

## 提交规范

格式：`<emoji> <type>: <中文描述>`

| Emoji | 类型 | 说明 |
|-------|------|------|
| ✨ | feat | 新功能 |
| 🔧 / 🐛 | fix | 修复 |
| 🎨 | fix | UI/样式修复 |
| 📝 | docs | 文档 |

- 描述使用简体中文
- 禁止出现 `Co-Authored-By: Claude` 等 AI 生成标识
- 一个提交解决一个问题，不做无关改动

## 数据库迁移（重要）

修改 Ent schema 后必须执行完整迁移流程：

```bash
# 1. 修改 schema 文件（internal/store/ent/schema/*.go）

# 2. 重新生成 Ent 代码
go generate ./internal/store/ent/...

# 3. 创建 Atlas 迁移（SQLite + PostgreSQL 各一份）
#    文件名格式：migrations/{sqlite,postgres}/YYYYMMDDHHMMSS_description.sql

# 4. 重算 atlas.sum 校验和（必须，否则 CI 失败）
go generate ./migrations/...

# 5. 验证迁移
go test ./migrations/...
```

注意事项：
- SQLite 和 PostgreSQL 迁移文件必须同步创建
- PostgreSQL 的 `integer`（int4）最大值 2,147,483,647；GitHub ID 已超过此值，相关字段必须用 `bigint`
- Ent 的 `field.Int64` 在 PostgreSQL 需要 `bigint` 迁移，不是 `integer`
- `atlas.sum` 校验和必须用 `go generate ./migrations/...` 重算，不可手写
- 迁移文件为不可变：已提交的迁移不要修改，只新增

## Ent ORM 约定

- Schema 定义在 `internal/store/ent/schema/`，修改后必须 `go generate`
- 领域模型在 `internal/store/domain.go`，Store 接口在同一文件
- 实现在 `internal/store/domain_stores.go`
- 物理表名通过 `entsql.Annotation{Table: "..."}` 固定，与 Atlas 迁移一致
- 新增字段需同步更新：schema → domain 模型 → store 实现 → fromEntity 映射

## 前端约定

- 路由：TanStack Router，定义在 `web/src/routes/`
- 数据获取：TanStack Query，queryOptions 模式
- API 客户端：`web/src/lib/api/client.ts`
- 类型定义：`web/src/features/monitor/api.ts` 导出共享类型
- 工具函数：`web/src/lib/format.ts`（如 formatRelativeTime）
- 侧边栏：`web/src/app/root-layout.tsx`
- 样式：`web/src/styles/globals.css`（纯 CSS，无 Tailwind utility class）

## CI/CD 发布流程

项目使用 **Tag 触发 CI 发布**：

```bash
# 1. 确保 VERSION 文件、CHANGELOG.md、README.md 版本号一致

# 2. 提交发布改动
git commit -m "✨ feat: ..."

# 3. 创建 annotated Tag
git tag -a v0.3.6 -m "v0.3.6"

# 4. 推送分支和 Tag
git push origin main
git push origin v0.3.6
```

CI 自动执行：test → build-and-push（v* Tag 触发双架构 amd64+arm64）→ 推送 GHCR

镜像标签规则：
- `v*` Tag → `vX.Y.Z` + `latest`
- `main` 分支 → `main` + `main-<sha>`
- `dev` 分支 → `dev` + `dev-<sha>`

## 版本号

- 位置：根目录 `VERSION` 文件（如 `0.3.6`）
- 遵循 SemVer
- 发布时需同步更新：`VERSION`、`CHANGELOG.md`、`README.md` 徽章

## 安全注意

- 不在代码或提交中暴露密钥、Token、Secret
- 敏感配置通过环境变量或主密钥 AES-GCM 加密
- Webhook 验签使用 HMAC-SHA256
- Session 使用 HttpOnly Cookie + 双提交 CSRF

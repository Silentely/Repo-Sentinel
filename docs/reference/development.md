# 开发规范

先准备固定工具链，再分别启动 Go API 和 Vite。生产构建把 Vite `dist` 嵌入 Go 二进制；普通开发构建使用仓库内的最小 fallback 页面。

相关页面：[从源码运行](/deploy/source) · [实现状态](/reference/implementation-status) · [系统架构](/reference/architecture)

## 使用固定工具链

- Go 1.26
- Node.js 24 或更高版本
- pnpm 10.34.5
- Atlas CLI 1.2.3（迁移校验和生成）
- Docker Engine 与 Compose v2（只在 PostgreSQL 契约需要）
- Chromium（只在 Playwright 浏览器旅程需要）

在 `web` 目录安装依赖，避免修改全局环境：

```bash
pnpm --dir "web" install
```

## 启动本地开发服务

先在仓库根目录启动 Go API：

```bash
mkdir -p ".tmp"
REPOSENTINEL_HTTP_ADDR="127.0.0.1:8080" \
REPOSENTINEL_DATABASE_DRIVER="sqlite" \
REPOSENTINEL_DATABASE_URL="file:./.tmp/reposentinel-dev.db" \
go run ./cmd/reposentinel serve
```

再启动 Vite：

```bash
pnpm --dir "web" dev
```

Vite 默认监听 `5173`，并把 `/api`、`/health` 代理到 `127.0.0.1:8080`。前端 API 客户端始终使用相对路径，因此同一套代码也能运行在生产嵌入服务中。

## 运行测试和静态检查

```bash
go test ./...
go test -race ./...
go vet ./...
go fmt ./...
pnpm --dir "web" test -- --run
pnpm --dir "web" typecheck
pnpm --dir "web" build
```

Go 测试默认使用 SQLite。PostgreSQL 契约需要可用的 Docker/数据库：

```bash
docker compose -f "deployments/test/postgres.compose.yml" up -d
REPOSENTINEL_TEST_POSTGRES_URL="postgres://reposentinel:reposentinel@127.0.0.1:55432/reposentinel_test?sslmode=disable" \
  go test ./internal/store/... -run PostgreSQL -v
```

仓库测试使用固定的 Atlas 1.2.3 Go 库同时校验两个迁移目录：

```bash
go test ./migrations -run TestAtlasMigrationDirectories -v
```

如果本机另外安装了 Atlas CLI，也可以运行 `atlas migrate validate --dir "file://migrations/sqlite"` 和对应的 PostgreSQL 命令。Atlas 1.2.3 的 Go 库模块不包含 `cmd/atlas` 可执行包，因此不要使用 `go run ariga.io/atlas/cmd/atlas@v1.2.3`。

如果本机没有 Docker、`psql` 或 `REPOSENTINEL_TEST_POSTGRES_URL`，请输出明确的 `SKIP`，不要把未执行写成通过。

## 构建 production embed

先生成带 hashed assets 的 Vite `dist`，再使用 `production` build tag：

```bash
pnpm --dir "web" build
OUTPUT=".tmp/reposentinel" BUILD_CHANNEL="local" make build-production
```

Makefile 从根目录 `VERSION` 和当前 Git 状态注入版本、提交、分支、UTC 构建时间与构建渠道。正式发布应把 `BUILD_CHANNEL` 设置为发布渠道，并在构建后运行 `".tmp/reposentinel" version` 核对元数据。`web/embed_production.go` 使用 `go:embed dist`；如果没有先生成 `dist/index.html`，production 编译会在 embed 阶段失败，它不会启动一个空白页面。普通 `go test` 和不带 tag 的开发构建使用 `web/fallback/index.html`。

SPA 处理规则如下：

- `index.html` 使用 `Cache-Control: no-cache`。
- hashed assets 使用 `public, max-age=31536000, immutable`，并按扩展名返回 Content-Type。
- 没有命中的浏览器路由回退到 `index.html`。
- `/api`、`/health` 以及路径穿越尝试永远返回 JSON 404。

## 运行 Playwright 认证旅程

脚手架会为桌面和移动项目各启动一个 production-tag 服务，并把 SQLite 文件放在被忽略的 `.test-run-data` 目录：

```bash
pnpm --dir "web" exec playwright test --list
pnpm --dir "web" e2e
```

`e2e` 命令会先构建前端。Chromium 浏览器由 Playwright 单独管理；项目不会自动安装它。若执行时报 `Executable doesn't exist`，记录：

```text
SKIP：Chromium/Playwright browser runtime 未安装
```

不要为了本地验证而进行系统级浏览器安装。

## 保持安全边界

- 不要把 Secret 写入 `configs/`、测试 fixture、截图或提交记录。
- 新增 HTTP 路径时，先决定它是否属于 `/api` 或 `/health` 保留前缀，避免被 SPA fallback 捕获。
- 新增静态资源时，保留 hashed 文件名和不可变缓存策略；`index.html` 必须保持可重新验证。
- 修改迁移时同时更新 SQLite/PostgreSQL 目录和 Atlas checksum，并运行两套校验。
- 代码注释保持简体中文，与现有包风格一致。

# RepoSentinel

RepoSentinel 是一个自托管的 GitHub 仓库值守平台。当前仓库交付的是 Phase 1 基础平台：配置与主密钥校验、SQLite/PostgreSQL 存储基础、唯一管理员、Session/CSRF、健康检查、CLI 恢复命令，以及 React 认证壳。

GitHub App 事件采集、规则分析、通知和真正的仓库仪表盘属于后续阶段。当前首页只展示服务端真实的 readiness、Session 和初始化状态，不会伪造仓库数据。

## 先运行起来

生产嵌入构建需要以下工具：

- Go 1.26
- Node.js 24 或更高版本
- pnpm 10.34.5

Docker、PostgreSQL、Playwright Chromium 只在对应的契约或浏览器验证中需要，不影响 SQLite 基础启动。

在仓库根目录运行：

```bash
mkdir -p ".tmp"
pnpm --dir "web" install
pnpm --dir "web" build
go build -tags production -o ".tmp/reposentinel" ./cmd/reposentinel
```

使用仓库内的 SQLite 文件启动：

```bash
REPOSENTINEL_HTTP_ADDR="127.0.0.1:8080" \
REPOSENTINEL_DATABASE_DRIVER="sqlite" \
REPOSENTINEL_DATABASE_URL="file:./.tmp/reposentinel.db" \
".tmp/reposentinel" serve
```

打开 <http://127.0.0.1:8080>，首次访问会进入初始化页面。创建唯一管理员后，使用该凭据登录。

可以先检查进程和数据库 readiness：

```bash
curl --fail "http://127.0.0.1:8080/health/live"
curl --fail "http://127.0.0.1:8080/health/ready"
curl --fail "http://127.0.0.1:8080/api/v1/setup/status"
```

## 配置和管理员操作

- [配置、环境变量与密钥轮换](docs/operations/configuration.md)
- [初始化、Session 与密码恢复](docs/operations/administrator-access.md)
- [开发、测试与 production embed](docs/operations/development.md)

配置优先级是默认值 → YAML 文件 → 环境变量。服务命令和恢复命令都支持 `--config` 指定 YAML 文件；Secret 不应写入仓库或提交记录。

## 开发命令

启动 Go API（默认监听 `127.0.0.1:8080`）后，可以在另一个终端启动 Vite：

```bash
pnpm --dir "web" dev
```

Vite 会把 `/api` 和 `/health` 代理到本机 Go 服务。常用验证命令：

```bash
go test ./...
go test -race ./...
go vet ./...
pnpm --dir "web" test -- --run
pnpm --dir "web" typecheck
pnpm --dir "web" build
```

生产二进制必须在前端构建完成后使用 `production` build tag 编译：

```bash
pnpm --dir "web" build
go build -tags production -o ".tmp/reposentinel" ./cmd/reposentinel
```

Playwright 测试脚手架可以列出测试而不需要浏览器：

```bash
pnpm --dir "web" exec playwright test --list
```

完整浏览器旅程需要本机已安装 Chromium。项目不会自动下载浏览器；缺少 runtime 时，将该项记录为 `SKIP`。

## 当前安全边界

- Session 原始令牌只通过 HttpOnly Cookie 返回；CSRF 令牌使用独立的非 HttpOnly Cookie 和请求头。
- 非安全 HTTP 方法需要 CSRF 校验；登录有本机内存限流。
- 首次 setup 默认只允许 loopback 请求。只有明确设置 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true` 才允许远程初始化，并且应放在 TLS 和网络访问控制之后。
- `/api` 与 `/health` 未知路径始终返回 JSON 404，不会被 SPA fallback 吞掉。
- 生产静态资源使用不可变缓存；`index.html` 使用 `no-cache`。

## 路线图

Phase 1 完成基础运行时和认证边界。后续阶段将接入 GitHub App、Webhook 事件、仓库规则、严重性聚合、通知渠道和可操作的仓库仪表盘；在这些能力落地前，文档和 UI 都会明确标注“后续阶段”。

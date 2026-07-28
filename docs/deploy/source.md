# 从源码运行

适合本地开发与自定义构建。生产更推荐 [Docker 部署](/deploy/docker)。

## 开发模式（API + Vite）

终端 1 — Go API：

```bash
mkdir -p .tmp
REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel-dev.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
go run ./cmd/reposentinel serve
```

终端 2 — 前端：

```bash
pnpm --dir web install
pnpm --dir web dev
```

Vite 默认 `5173`，并将 `/api`、`/health` 代理到 `127.0.0.1:8080`。前端客户端使用相对路径，与嵌入式生产模式一致。

## 生产式单二进制

```bash
pnpm --dir web install
pnpm --dir web build
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
```

- 带 `production` build tag 时嵌入 `web/dist`
- 开发 tag 使用最小 fallback HTML，避免误把未构建前端当生产

运行：

```bash
REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
REPOSENTINEL_PUBLIC_BASE_URL=https://monitor.example.com \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:/data/reposentinel.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
.tmp/reposentinel serve
```

公网请始终配置正确的 `REPOSENTINEL_PUBLIC_BASE_URL`（影响 Cookie `Secure` 等），并置于 HTTPS 反向代理之后。见 [反向代理](/deploy/reverse-proxy)。

## PostgreSQL

```bash
docker compose -f deployments/test/postgres.compose.yml up -d
REPOSENTINEL_DATABASE_DRIVER=postgres \
REPOSENTINEL_DATABASE_URL="postgres://reposentinel:reposentinel@127.0.0.1:55432/reposentinel_test?sslmode=disable" \
.tmp/reposentinel serve
```

测试 compose **不是**生产加固模板；生产库请自备网络隔离、密码与备份。

## 配置文件

```bash
.tmp/reposentinel config validate --config configs/reposentinel.example.yaml
.tmp/reposentinel serve --config configs/reposentinel.example.yaml
```

Secret 不要写入仓库内 YAML。完整变量表见 [配置参考](/reference/configuration)。

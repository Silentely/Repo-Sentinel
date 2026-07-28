# 快速开始

本指南帮助你在本机构建二进制、启动 SQLite 实例、创建唯一管理员并登录。生产环境更推荐 [Docker 部署](/deploy/docker)。

## 1. 准备环境

| 需求 | 说明 |
|------|------|
| Go 1.26+ | 后端与 CLI |
| Node.js 24+ | 前端构建 |
| pnpm 10.34.5 | `web/packageManager` 已锁定 |
| 可选 sqlite3 | SQLite 在线备份 |
| 可选 Docker | PostgreSQL 契约或 Compose 部署 |

## 2. 安装前端依赖并构建

在仓库根目录：

```bash
pnpm --dir web install
pnpm --dir web build
```

## 3. 编译服务

开发构建：

```bash
mkdir -p .tmp
make build OUTPUT=.tmp/reposentinel
```

生产嵌入构建（需先完成前端 build）：

```bash
OUTPUT=.tmp/reposentinel BUILD_CHANNEL=local make build-production
```

确认版本：

```bash
.tmp/reposentinel version
```

## 4. 最小启动（SQLite）

```bash
mkdir -p .tmp
REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
REPOSENTINEL_GITHUB_WEBHOOK_SECRET=dev-secret \
.tmp/reposentinel serve
```

打开 <http://127.0.0.1:8080>：

1. 首次访问进入 **创建唯一管理员**
2. 创建成功后自动登录
3. 在仪表盘查看健康、仓库与事件（配置 Webhook 后有数据）

## 5. 健康检查

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
curl -fsS http://127.0.0.1:8080/api/v1/setup/status
```

## 6. 可选：环境变量预置管理员

```bash
read -r -s REPOSENTINEL_ADMIN_PASSWORD
export REPOSENTINEL_ADMIN_PASSWORD
REPOSENTINEL_ADMIN_USERNAME="admin" \
REPOSENTINEL_HTTP_ADDR=127.0.0.1:8080 \
REPOSENTINEL_DATABASE_DRIVER=sqlite \
REPOSENTINEL_DATABASE_URL=file:./.tmp/reposentinel.db \
REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
.tmp/reposentinel serve
unset REPOSENTINEL_ADMIN_PASSWORD
```

用户名与密码必须成对出现；已有管理员时不会用环境变量覆盖密码。

## 7. 下一步

- [管理员与 Session](/guide/administrator)
- [配置参考](/reference/configuration)
- [Docker 部署](/deploy/docker)
- [从源码运行](/deploy/source)

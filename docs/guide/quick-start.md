# 快速开始

本指南帮助你在本机跑通 **Phase 1**：构建二进制、启动 SQLite 实例、创建唯一管理员并登录。

::: warning 能力边界
当前版本**不会**接收 GitHub Webhook，也**不会**发送 Telegram 通知。若你需要完整监控能力，请先阅读 [功能与路线图](/features) 与 [实现状态](/reference/implementation-status)。
:::

## 1. 准备环境

| 需求 | 说明 |
|------|------|
| Go 1.26+ | 后端与 CLI |
| Node.js 24+ | 前端构建 |
| pnpm 10.34.5 | `web/packageManager` 已锁定 |
| 可选 sqlite3 | SQLite 在线备份 |
| 可选 Docker | 仅 PostgreSQL 契约测试 |

## 2. 安装前端依赖并构建

在仓库根目录：

```bash
pnpm --dir web install
pnpm --dir web build
```

## 3. 编译服务

开发构建（无完整前端嵌入时使用 fallback 页）：

```bash
mkdir -p .tmp
make build OUTPUT=.tmp/reposentinel
```

生产嵌入构建（需先完成 `pnpm --dir web build`）：

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
.tmp/reposentinel serve
```

打开 <http://127.0.0.1:8080>：

1. 首次访问进入 **创建唯一管理员**
2. 创建成功后自动登录
3. 仪表盘展示真实 readiness 与上手步骤（后续阶段能力会标为「下一阶段」）

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
.tmp/reposentinel serve
unset REPOSENTINEL_ADMIN_PASSWORD
```

用户名与密码必须成对出现；已有管理员时不会用环境变量覆盖密码。

## 7. 下一步

- [管理员与 Session](/guide/administrator) — setup 远程开关、改密、CLI 重置
- [配置参考](/reference/configuration) — 主密钥、数据库、日志
- [从源码运行](/deploy/source) — 前后端分离开发与 production embed

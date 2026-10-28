# Docker 部署

生产推荐：拉取 GHCR 镜像 + 数据卷（默认 SQLite）+ 反向代理 HTTPS。  
本地 `docker build` 只用于开发排障：自行构建要自己维护版本注入与安全更新，容易和发布渠道脱节。

## 镜像

```text
ghcr.io/silentely/repo-sentinel:latest
```

也可固定版本，例如 `ghcr.io/silentely/repo-sentinel:v0.9.0`。  
若包为私有，先登录：

```bash
echo "$GITHUB_TOKEN" | docker login ghcr.io -u USERNAME --password-stdin
```

维护者发版与标签说明见 [发布与镜像](/reference/release)（用户部署可跳过）。

## 1. 准备环境变量

```bash
cp .env.example .env
```

必填与强烈建议：

| 变量 | 必要性 | 说明 |
|------|--------|------|
| `REPOSENTINEL_ENCRYPTION_KEY` | 必填 | 主密钥，`openssl rand -base64 32` 生成，**必须**与数据一并备份 |
| `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` | 必填 | GitHub App Webhook Secret；未配置时入站 Webhook 全部拒绝 |
| `REPOSENTINEL_PUBLIC_BASE_URL` | 强烈建议 | 对外访问地址，生产用 `https://你的域名`；留空时 Cookie 不会带 `Secure`，Agent 发现链接也会指向本机地址 |

建议同时配置管理员（否则首次用本机浏览器 Web setup）：

```bash
REPOSENTINEL_ADMIN_USERNAME=admin
REPOSENTINEL_ADMIN_PASSWORD=请换成强密码
```

经域名 / 反向代理做首次 setup 时，需临时 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`，完成后关掉。  
完整变量表见 [配置参考](/reference/configuration)。

## 2. 数据库：SQLite（默认）或 PostgreSQL

Compose 默认已是 **SQLite**，数据落在卷 `reposentinel-data` 的 `/data/reposentinel.db`。`docker-compose.yml` 里的写法：

```yaml
environment:
  REPOSENTINEL_DATABASE_DRIVER: "${REPOSENTINEL_DATABASE_DRIVER:-sqlite}"
  REPOSENTINEL_DATABASE_URL: "${REPOSENTINEL_DATABASE_URL:-file:/data/reposentinel.db}"
volumes:
  - reposentinel-data:/data
```

改用 **PostgreSQL** 时，在 `.env` 或 compose `environment` 中设置：

```bash
REPOSENTINEL_DATABASE_DRIVER=postgres
REPOSENTINEL_DATABASE_URL=postgres://用户:密码@db:5432/reposentinel?sslmode=require
```

说明：

- `driver` 只能是 `sqlite` 或 `postgres`（不要写 `postgresql`）
- Postgres **必须**提供非空 `REPOSENTINEL_DATABASE_URL`
- **密码中的 `#` `@` `(` 等必须 URL 编码**，否则启动即 `database_unavailable`（见 [配置参考](/reference/configuration)）
- 库需事先建好；应用启动时自动跑迁移
- 仓库里的 `deployments/test/postgres.compose.yml` 仅供开发测试，**不是**生产模板

可选连接池（仅 Postgres 有意义）：

```bash
REPOSENTINEL_DATABASE_MAX_OPEN_CONNS=10
REPOSENTINEL_DATABASE_MAX_IDLE_CONNS=5
```

## 3. 启动

### Docker Compose（推荐）

根目录 `docker-compose.yml` 默认镜像为 `ghcr.io/silentely/repo-sentinel:latest`。

```bash
docker compose pull
docker compose up -d
curl -fsS http://127.0.0.1:8080/health/ready
```

### docker run

```bash
docker run -d \
  --name reposentinel \
  --restart unless-stopped \
  -p 8080:8080 \
  -v reposentinel-data:/data \
  -e REPOSENTINEL_HTTP_ADDR=0.0.0.0:8080 \
  -e REPOSENTINEL_PUBLIC_BASE_URL=https://monitor.example.com \
  -e REPOSENTINEL_DATABASE_DRIVER=sqlite \
  -e REPOSENTINEL_DATABASE_URL=file:/data/reposentinel.db \
  -e REPOSENTINEL_ENCRYPTION_KEY="$(openssl rand -base64 32)" \
  -e REPOSENTINEL_GITHUB_WEBHOOK_SECRET=your-webhook-secret \
  ghcr.io/silentely/repo-sentinel:latest
```

> 首次部署才这样生成主密钥。之后**重复执行同一条命令会换一把新密钥**，而数据卷里的密文仍用旧密钥加密，启动会报 `encryption_key_mismatch`。重建容器时请把已生成的密钥固定写进 `.env`，不要每次现算。

### 自检

```bash
curl -fsS http://127.0.0.1:8080/health/live
curl -fsS http://127.0.0.1:8080/health/ready
# 仅 Compose 部署适用；docker run 用 docker exec reposentinel /reposentinel version
docker compose exec reposentinel /reposentinel version
```

浏览器打开应用地址，完成管理员创建（若未用环境变量预置）。公网请接 [反向代理](/deploy/reverse-proxy)。

## 4. GitHub App（创建表单逐项）

RepoSentinel **不走用户 OAuth 登录 GitHub**；认证是「App JWT → Installation Token」。因此创建页里与「Identifying and authorizing users」相关的 Callback / Device Flow 均可空着。

打开 [Create GitHub App](https://github.com/settings/apps/new)（组织可用 `https://github.com/organizations/<org>/settings/apps/new`）。

### 4.1 基本信息

| 字段 | 怎么填 |
|------|--------|
| **GitHub App name** | 任意全局唯一名，例如 `RepoSentinel` 或 `RepoSentinel-<你的ID>` |
| **Description** | 可选，如「自托管仓库值守」 |
| **Homepage URL** | 你的管理台公网地址，如 `https://monitor.example.com`；本地可先填 `http://127.0.0.1:8080`（Webhook 仍需 GitHub 能访问的 HTTPS） |

### 4.2 Identifying and authorizing users（可全部跳过）

| 字段 | 怎么填 |
|------|--------|
| **Callback URL** | **留空**（App 自身不使用 OAuth 回调；`/oauth/authorize` 只是声明端点，不支持交互式授权流） |
| **Expire user authorization tokens** | 不勾 |
| **Request user authorization (OAuth) during installation** | **不勾** |
| **Enable Device Flow** | **不勾** |

### 4.3 Post installation

| 字段 | 怎么填 |
|------|--------|
| **Setup URL** | 可选；可填管理台地址，安装后跳回你的控制台 |
| **Redirect on update** | 可选；一般不勾 |

### 4.4 Webhook（必填）

| 字段 | 怎么填 |
|------|--------|
| **Active** | **必须勾选** |
| **Webhook URL** | `https://你的域名/webhooks/github`（与 `REPOSENTINEL_PUBLIC_BASE_URL` + 路径一致；管理台「GitHub App」页可复制） |
| **Secret** | 随机串，例如 `openssl rand -hex 32`；**同一值**写入环境变量 `REPOSENTINEL_GITHUB_WEBHOOK_SECRET` |

> 本地开发可用 [smee.io](https://smee.io) / ngrok 等把公网 HTTPS 转到本机；Secret 仍要与环境变量一致。

### 4.5 Repository permissions（按功能授权）

要正常使用 Issue、PR 与 Actions 监控，请将下面七项权限设为 **Read-only**。PR 的 CI 状态查询同时读取 Checks 和 Commit statuses；只授权 Actions 无法替代这两项权限。

| 权限 | Access | 用途 |
|------|--------|------|
| **Metadata** | Read-only | GitHub App 必选，读取仓库基础信息 |
| **Contents** | Read-only | 读取提交、Release 与仓库内容，支持对账和代码审查 |
| **Issues** | Read-only | Issue Webhook 与 API 对账 |
| **Pull requests** | Read-only | PR Webhook、对账、读取 Diff 与审查信息 |
| **Checks** | Read-only | 读取 PR 提交上的 Check runs 与 CI 检查结果 |
| **Commit statuses** | Read-only | 读取 PR 提交上的状态检查，与 Checks 一起供 GraphQL CI 状态查询使用 |
| **Actions** | Read-only | 接收 workflow_run 事件，读取工作流、任务与失败日志 |

其中 Metadata 为必选权限，其余六项构成常用监控功能的只读权限集。若关闭某类功能，可按实际使用范围减少授权；启用 PR 监控时请保留 Checks 和 Commit statuses。

开启安全告警监控时，再按需添加：

| 权限 | Access | 用途 |
|------|--------|------|
| **Dependabot alerts** | Read-only | 依赖漏洞告警 |
| **Code scanning alerts** | Read-only | 代码扫描告警 |
| **Secret scanning alerts** | Read-only | 泄露凭据告警 |

安全告警还受仓库是否启用相应扫描功能、仓库可见性及 GitHub 套餐限制。授权本身不会启用扫描服务。

只有启用以下写入功能时，才需要将对应权限提高到 **Read and write**：

| 功能 | 需要提高的权限 |
|------|----------------|
| PR 审查报告评论回写（`REPOSENTINEL_AI_CODE_REVIEW_COMMENT_ON_PR=true`） | **Issues**；PR 下的普通评论通过 Issue 评论接口发布 |
| Issue 自动标签、ChatOps 回复、打标签、标记重复、标记垃圾并关闭等操作 | **Issues** |
| ChatOps 快捷批准 PR（提交 APPROVE review） | **Pull requests** |
| ChatOps 重跑工作流 | **Actions** |

- **Organization permissions**：全部 **No access**，除非另外使用需要组织权限的能力。
- **Account permissions**：全部 **No access**。
- 只读监控无需申请写权限。评论回写失败会记录告警，不影响已生成的审查报告和通知。

#### 已安装 App 增加权限后，让授权生效

1. 在 GitHub App 的 **Permissions & events** 中修改并保存权限。
2. 进入安装所属账号或组织的 **Settings → Applications / GitHub Apps → Installed GitHub Apps → Configure**，按 GitHub 提示批准新增权限。组织安装通常需要有权限的组织管理员操作。
3. 确认安装选择的仓库包含所有需要监控的仓库。权限设置正确，但仓库不在授权范围内，也会出现 `Resource not accessible by integration`。
4. 已缓存的 installation token 可能仍带旧权限。需要立即验证时，在批准权限后重启 RepoSentinel，让它重新签发 token；否则等待缓存 token 刷新（通常在一小时内）。健康探针结果还会缓存 60 秒。
5. 在管理台 **GitHub App** 页点击 **从 GitHub 同步仓库**，更新仓库列表，再对目标仓库执行对账。同步仓库列表不能代替 GitHub 侧批准新增权限。

收到 Webhook 只说明入站链路正常，不能证明主动调用 API 的权限齐全。出现 `graphql_permission_denied: Resource not accessible by integration` 时，优先核对目标仓库授权，以及 Pull requests、Contents、Checks、Commit statuses 的安装权限。

### 4.6 Subscribe to events（勾选）

| 事件 | 是否需要 |
|------|----------|
| **Issues** | 要 |
| **Pull request** | 要 |
| **Workflow run** | 要 |
| **Dependabot alert** | 要（安全） |
| **Code scanning alert** | 要（安全） |
| **Secret scanning alert** | 要（安全） |
| **Installation** | 要 |
| **Installation repositories** | 要 |
| **Repository** | 要 |
| **Installation target** | 可不勾 |
| **Meta** | 可不勾 |
| **Security advisory** | 可不勾 |

未订阅的事件类型不会送入处理。上面标「可不勾」的三类即便勾上也不会报错，应用收到投递后直接忽略。

创建页若暂时看不到某事件，先提高对应 Repository permission，保存后再回来勾事件。

### 4.7 Where can this GitHub App be installed?

| 选项 | 建议 |
|------|------|
| **Only on this account** | 个人自用推荐 |
| **Any account** | 需要装到多个组织/账号时再选 |

### 4.8 创建后必做

1. **Generate a private key**，下载 `.pem`  
2. 记下 **App ID** 与 **Client ID**（对账核心依赖 **App ID + 私钥**）  
3. **写入运行时**，二选一：  
   - **方式 A · 管理台（推荐上手）**：登录后打开 **GitHub App** 页，填写 App ID / Client ID / Public Base URL / Webhook Secret，并粘贴 PEM 或填服务器路径 → **保存**（加密入库，立即生效，无需重启）。  
   - **方式 B · 环境变量（适合 Docker / K8s Secret）**：

     ```bash
     REPOSENTINEL_GITHUB_APP_ID=123456
     REPOSENTINEL_GITHUB_CLIENT_ID=Iv1.xxxxxxxx
     REPOSENTINEL_GITHUB_PRIVATE_KEY_PATH=/secrets/github-app.pem
     REPOSENTINEL_GITHUB_WEBHOOK_SECRET=与 App Webhook Secret 相同
     REPOSENTINEL_PUBLIC_BASE_URL=https://你的域名
     ```

     compose 中把 pem 挂载到该路径；或改用管理台粘贴 PEM。

   > **优先级**：环境变量 **高于** 管理台数据库配置。某字段已用环境变量设置时，管理台该字段会显示「锁定」，不能覆盖。

4. **Install App**（在 GitHub 完成，管理台不能代替授权）  
   - 打开 [已安装的 Apps](https://github.com/settings/installations) 或 App 设置页的 Install  
   - 选择 All / 指定仓库后保存；GitHub 会向 `/webhooks/github` 推送 `installation`  
5. **确认入库**  
   - 管理台 **GitHub App** → Installation 列表出现账号  
   - **仪表盘** 出现仓库（状态「基线中」）  
   - 若日志已 `accepted` 但仪表盘仍空：点 **「从 GitHub 同步仓库」**（`POST /api/v1/github/sync-repositories`）补拉  
6. 对需要监控的仓点 **「立即放行」** 后再发实时通知

## 5. Telegram / 其他通知

```bash
REPOSENTINEL_TELEGRAM_TOKEN=...
REPOSENTINEL_TELEGRAM_CHAT_ID=...
# 或
# REPOSENTINEL_HTTP_WEBHOOK_URL=https://hooks.example.com/...
# REPOSENTINEL_HTTP_WEBHOOK_SECRET=...
```

也可登录后在「渠道配置」页保存。Token 用主密钥加密入库。

## 6. 备份

备份输出要落在数据卷 `/data` 下。容器的 `/tmp` 没有挂载卷，写在那里会在容器重建时丢失：

```bash
# 在维护窗口执行；同时保管 REPOSENTINEL_ENCRYPTION_KEY
docker compose exec reposentinel /reposentinel backup \
  --output /data/reposentinel-backup.db
# 拷到宿主机留存
docker compose cp reposentinel:/data/reposentinel-backup.db ./
```

每次备份换一个文件名，避免覆盖上一份。容器内 `backup` / `restore` 仅适用于 SQLite；PostgreSQL 请从容器外部直连数据库执行。详见 [运维手册](/reference/ops)。

## 本地自建镜像（可选）

仅开发或排障：

```bash
docker build -t reposentinel:local \
  --build-arg VERSION="$(tr -d '[:space:]' < VERSION)" \
  --build-arg BUILD_CHANNEL=local .
```

生产请继续用 `ghcr.io/silentely/repo-sentinel:latest`（或固定为具体的 `vX.Y.Z`）。

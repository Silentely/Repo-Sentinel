# 反向代理

RepoSentinel 自身提供 HTTP 服务。公网部署应由 Caddy / Nginx / Traefik 终止 TLS，并反代到应用监听地址。

## 通用要求

- 把 `Host`、`X-Forwarded-For`、`X-Forwarded-Proto` 正确传给后端；若只在代理层终止 TLS，还要给应用配 `REPOSENTINEL_PUBLIC_BASE_URL=https://你的域名`
- 限制管理面来源 IP（若适用）
- **首次 setup**：默认仅 loopback；经域名初始化必须 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`，完成后关闭
- Webhook 路径：`/webhooks/github` 需对 GitHub 可达（通常对公网开放，管理面可另做 IP 限制）

## 让应用识别客户端真实 IP

应用默认只认直连 IP，**忽略** `X-Forwarded-For` / `X-Real-IP`。反代之后若不配置受信任代理，登录限流与审计日志记录的都会是代理地址（如 `127.0.0.1`）而不是访客 IP。

把反代自身的地址加进受信任列表，应用才会回溯这些请求头：

```bash
# 反代与应用同机时
REPOSENTINEL_HTTP_TRUSTED_PROXIES="127.0.0.1"
# 反代在另一台机器或 Kubernetes 内时，填反代出口所在的子网
REPOSENTINEL_HTTP_TRUSTED_PROXIES="10.0.0.0/8,172.16.0.0/12"
```

配置文件写法与解析规则见 [管理员与 Session](/guide/administrator#受信任反向代理与-ip-解析)。只应信任你自己的代理，不要图省事写 `0.0.0.0/0`——那等于允许访客伪造 IP 绕过限流。

## Caddy 示例

```text
monitor.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

Caddy 的 `reverse_proxy` 默认就会带上 `X-Forwarded-For`、`X-Forwarded-Proto` 与 `X-Forwarded-Host`，无需手写。

## Nginx 示例

```nginx
server {
  listen 443 ssl http2;
  server_name monitor.example.com;

  # ssl_certificate ...;
  # ssl_certificate_key ...;

  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;
  }
}
```

## Traefik 示例

用 Docker label 接入（Traefik 会自动设置 `X-Forwarded-*`）：

```yaml
labels:
  - "traefik.enable=true"
  - "traefik.http.routers.reposentinel.rule=Host(`monitor.example.com`)"
  - "traefik.http.routers.reposentinel.entrypoints=websecure"
  - "traefik.http.routers.reposentinel.tls.certresolver=letsencrypt"
  - "traefik.http.services.reposentinel.loadbalancer.server.port=8080"
```

要让应用拿到访客 IP，还需按上一节配置 `REPOSENTINEL_HTTP_TRUSTED_PROXIES`；否则即使代理把请求头传对了，应用仍取直连 socket IP。

## Telegram Web Mini App (TMA) 部署

当启用 Telegram Web Mini App (`REPOSENTINEL_TMA_ENABLED=true`) 时，需注意以下反向代理配置：

### 强制 HTTPS 要求

Telegram 客户端强制要求所有 Mini App 必须运行在 HTTPS 环境下，否则 Telegram 客户端将拒绝加载页面并抛出安全警告：
- `REPOSENTINEL_PUBLIC_BASE_URL` 必须配置为以 `https://` 开头的公开域名访问地址（例如 `https://monitor.example.com`）。若开启 TMA 且基础地址为非 HTTPS，应用启动校验将直接报错阻断。
- 反向代理必须正确向后端透传 `X-Forwarded-Proto: https` 以及 `X-Forwarded-For`，以便后端能够正确完成跨站 Origin 校验及独立滑动窗口限流。

### Nginx 配置示例（支持 /tma 与 /api/v1/tma）

针对 `/tma`（SPA 路由）及 `/api/v1/tma`（后端接口），需要透传请求头并配置 WebSocket/流式响应（以适配实时日志查看与 SSE 推送）：

```nginx
# 顶层 http 块中配置 WebSocket 升级映射
map $http_upgrade $connection_upgrade {
  default upgrade;
  ''      close;
}

server {
  listen 443 ssl http2;
  server_name monitor.example.com;

  ssl_certificate /path/to/fullchain.pem;
  ssl_certificate_key /path/to/privkey.pem;

  # 全局与 TMA 路径反向代理
  location / {
    proxy_pass http://127.0.0.1:8080;
    proxy_set_header Host $host;
    proxy_set_header X-Real-IP $remote_addr;
    proxy_set_header X-Forwarded-For $proxy_add_x_forwarded_for;
    proxy_set_header X-Forwarded-Proto $scheme;

    # WebSocket / SSE 流式长连接与升级透传
    proxy_http_version 1.1;
    proxy_set_header Upgrade $http_upgrade;
    proxy_set_header Connection $connection_upgrade;

    # 关闭缓冲以支持流式日志实时输出
    proxy_buffering off;
    proxy_cache off;
    proxy_read_timeout 300s;
  }
}
```

### Caddy 配置示例

Caddy 自带自动化 HTTPS 证书申请，针对 `/tma` 与 `/api/v1/tma` 的流式日志与 WebSocket，建议显式配置立即刷新：

```text
monitor.example.com {
  reverse_proxy 127.0.0.1:8080 {
    # 保持 WebSocket/SSE 流式连接，禁用缓冲
    flush_interval -1
  }
}
```

## 容器部署

见 [Docker 部署](/deploy/docker)：单应用容器、`/health/ready`、数据卷与 Compose 安全基线。

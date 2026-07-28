# 反向代理

RepoSentinel Phase 1 自身只提供 HTTP 服务。公网部署应由 Caddy / Nginx / Traefik 终止 TLS，并反代到应用监听地址。

## 通用要求

- 将 `Host` 与客户端协议正确传给后端（或仅在代理层终止 TLS，应用配置 `REPOSENTINEL_PUBLIC_BASE_URL=https://你的域名`）
- WebSocket 非 Phase 1 需求；后续若增加实时通道再补升级说明
- 限制管理面来源 IP（若适用）
- **首次 setup**：默认仅 loopback；经域名初始化必须 `REPOSENTINEL_SETUP_ALLOW_REMOTE=true`，完成后关闭

## Caddy 示例

```text
monitor.example.com {
  reverse_proxy 127.0.0.1:8080
}
```

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

应用需能从 `X-Real-IP` / `X-Forwarded-For` 识别客户端（已有 real IP 中间件）；确保只信任来自本机代理的连接。

## 后续：Docker

规格要求单应用容器、`/health/ready` Healthcheck、只读根文件系统等。**镜像与 Compose 生产文件尚未发布**；就绪后本页将增加 Docker 专章（对齐 TG-SignPulse 的 `deploy/docker` 结构）。

# 安全策略

## 支持的版本

| 版本 | 支持 |
|------|------|
| 最新 `main` / 最新正式 `v*` Release | 接受安全修复 |
| 更旧的 minor / patch | 视情况；重大问题可能仅在最新版修复 |

生产请使用 [GitHub Releases](https://github.com/Silentely/Repo-Sentinel/releases) 或 GHCR 标签 `latest` / 钉死的 `vX.Y.Z`。

## 报告漏洞

请**不要**通过公开 Issue 或讨论区披露未修复漏洞。

推荐方式（任选其一）：

1. 使用 GitHub 仓库的 **Security Advisories / 私密漏洞报告**（若已开启）
2. 向仓库维护者发送私信或私密渠道说明（GitHub 用户 [Silentely](https://github.com/Silentely)）

报告请尽量包含：

- 影响版本（`VERSION` 或镜像标签）
- 复现步骤与环境（自托管 / Docker）
- 实际影响（例如未授权访问、SSRF、密钥泄露）
- 是否已在公网实例上验证

我们会确认后给出修复计划或缓解措施；请在修复发布前避免公开细节。

## 部署侧安全基线（自托管）

- 使用强随机 `REPOSENTINEL_ENCRYPTION_KEY`，并与数据库备份一并离线保管
- 生产 `REPOSENTINEL_PUBLIC_BASE_URL` 使用 HTTPS；反向代理终止 TLS
- 限制管理面与 `/metrics` 的网络暴露；为 metrics 设置 Bearer Token
- 出站 HTTP 通知渠道默认禁止私网目标；仅在可信内网开启 `allow_private`
- 定期轮换 GitHub Webhook Secret 与 Telegram Bot Token
- 不要把 `.env`、App 私钥、备份文件提交到 Git

## 依赖与供应链

- Go 模块：`go.mod` / `go.sum`
- 前端：`web/pnpm-lock.yaml`
- 镜像：仅信任 `ghcr.io/silentely/repo-sentinel` 及本仓库 Actions 构建结果
- Dependabot（若启用）会定期开 PR 升级依赖；合并前请跑测试

## 致谢

负责任的披露有助于保护所有自托管用户。感谢你的报告。

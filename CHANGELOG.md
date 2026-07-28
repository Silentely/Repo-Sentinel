# Changelog

本文件遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 SemVer。

## [0.3.3] - 2026-07-28

### Changed

- Docker Compose 默认使用 `ghcr.io/silentely/repo-sentinel:latest`，部署无需本地构建
- GHCR 工作流：`main` 推送只跑测试；正式 `v*` 一次推送 `vX.Y.Z` / `latest` / `main` 等标签；`dev` 推送预发镜像
- 用户文档去掉与内部计划/草稿文档的交叉引用；部署说明以 GHCR 拉取为主

### Fixed

- 迁移失败时保留底层错误信息，便于排障

## [0.3.2] - 2026-07-28

### Added

- 关于页「检查更新」：优先 GitHub HTML `releases/latest` 302 解析 tag，失败再回退 API JSON；可关；失败 soft-fail
- `POST /api/v1/system/version/check` 与版本响应字段 `update_check_enabled`
- `syncx` 外部仓轮询与 `digest` 每日摘要单元测试；聚合多实例时间桶幂等测试

### Changed

- 通知合并 Outbox 幂等键改为「渠道 + 仓 + 类别 + 时间桶」，多实例下重复合并通知可收敛
- 出站 HTTP 客户端在拨号时 pin 解析后的公网 IP，降低 DNS rebinding 风险
- 聚合窗口等可通过 `REPOSENTINEL_AGGREGATION_*` 配置

### Fixed

- 文档版本号与 `VERSION` 一致；运维手册标明 backup/restore、`/metrics` 已实现

## [0.3.1] - 2026-07-28

### Fixed

- 修正 Ent 物理表名与 Atlas 迁移不一致（如 `notification_outbox`），恢复通知 Outbox 等写库路径
- 修复通知聚合器在持锁时访问数据库可能导致的死锁风险
- 出站 HTTP Webhook 禁止跟随重定向，并加强私网/元数据 SSRF 校验
- 聚合通知 HTML 文本转义，避免特殊字符破坏 Telegram 解析
- SQLite 备份改为参数化 `VACUUM INTO`，避免路径拼接
- 补充 Webhook 相关 API 错误码中文说明

### Added

- 通知聚合与超频摘要、仓库同步调度、外部公开仓轮询、每日摘要
- `doctor` / `backup` / `restore` CLI
- GHCR 镜像构建工作流与 Prometheus `/metrics` 端点
- 管理后台 Issues/PR、Actions、安全告警、GitHub App、关于页面

## [0.3.0] - 2026-07-28

### Added

- Webhook 验签接收、事件规范化、规则引擎、Telegram/HTTP 通知
- 管理仪表盘与渠道配置
- Docker Compose 与文档站

## [0.2.0] - 2026-07-28

### Added

- 基础认证、配置、双数据库迁移与管理壳

## [0.1.0] - 2026-07-28

### Added

- 项目初始骨架

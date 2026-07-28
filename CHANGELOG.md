# Changelog

本文件遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 SemVer。

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

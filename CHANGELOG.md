# Changelog

本文件遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 SemVer。

## [0.3.6] - 2026-07-29

### Added

- 列表本地忽略：Issue / PR / Actions / 安全告警可标记忽略（不回写 GitHub），支持「关注中 / 已忽略」切换
- Issues / PR / Actions / 安全告警页增加按仓库筛选
- 忽略 API：`PATCH /api/v1/work-items/{id}/ignored`、`PATCH /api/v1/workflow-runs/{id}/ignored`、`PATCH /api/v1/security-alerts/{id}/ignored`
- 列表查询参数 `ignored=true|all`；默认排除已忽略项
- 数据库迁移 `20260729000300_item_ignored`（PostgreSQL + SQLite）
- 仪表盘区块（通知投递 / 最近事件 / 仓库与基线）支持折叠，状态写入 localStorage

### Fixed

- 侧边栏导航徽章数字被半透明规则污染导致看不清
- 已归档仓库的历史 Issue / PR / Actions / 告警仍出现在列表与侧栏计数中
- 仪表盘「仓库与基线」仍展示已归档仓库；动作按钮列不对齐
- Actions 空状态未说明权限/事件/对账排查路径；请求失败时静默空白
- 仪表盘 `open_issues` 计数误含 open PR

### Changed

- 列表与仪表盘统计默认排除已归档仓库与已忽略项
- 仪表盘区块顺序调整为：通知投递 → 最近事件 → 仓库与基线
- 长标题单行截断、事件/投递行改为主内容 + 右侧动作布局

## [0.3.5] - 2026-07-29

### Added

- 仓库能力开关：单仓独立控制监控、Issues、PR、Actions、安全告警的开关
- 仓库归档功能：管理台一键归档/取消归档，联动同步状态；归档自动关闭所有开关
- 仓库管理页分开展示关注中/已归档仓库，默认显示关注中
- 侧边栏 Issues / Pull Requests 拆分为独立页面，支持 Open/Closed 状态筛选，默认显示 Open
- 安全告警页增加 Dependabot / Code Scanning / Secret Scanning 分类筛选，默认显示 Open
- 已关闭/已忽略项目显示数量限制：系统设置可配置（默认 20 条），避免历史数据无限增长
- 仓库管理页面：集中管理所有仓库的能力开关与归档状态
- GitHub App 页面精简：表单指南和安装步骤改为折叠面板，默认收起
- 修复侧边栏随主内容滚动，改为固定定位
- `PATCH /api/v1/repositories/{id}/settings` API 端点
- 数据库迁移 `20260729000100_repo_capability_toggles`（PostgreSQL + SQLite）

### Fixed

- 修复 `workflow_run` Webhook 处理时 GitHub 偶发缺字段导致数据库写入失败
- 修复 `mapStoreError` 吞掉原始错误信息，现在保留完整错误链便于排障
- 修复 `handleActivateRepository` 未检查 Upsert 返回错误

### Changed

- `Upsert` 不再覆盖用户配置的仓库能力开关，能力开关仅通过 `UpdateSettings` 修改

## [0.3.4] - 2026-07-28

### Changed

- GHCR 标签：`main` → `main` + `main-<sha>`；`dev` → `dev` + `dev-<sha>`；正式 `v*` → `vX.Y.Z` + `latest`（双架构）
- 补充项目协作文档：CONTRIBUTING、SECURITY、PR / Issue 模板、Dependabot、发布说明

### Fixed

- 文档与工作流描述与上述标签规则一致

## [0.3.3] - 2026-07-28

### Changed

- Docker Compose 默认使用 `ghcr.io/silentely/repo-sentinel:latest`，部署无需本地构建
- 用户文档去掉与内部草稿的交叉引用；部署说明以 GHCR 拉取为主

### Fixed

- 迁移失败时保留底层错误信息，便于排障
- SQLite Atlas 迁移锁名按连接隔离，避免并行测试争锁导致 CI 失败

### Notes

- 本版曾尝试调整 GHCR 策略；最终标签规则以 **0.3.4** 为准（见上）

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

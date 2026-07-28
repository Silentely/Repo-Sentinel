# 功能介绍

> 当前版本以仓库根目录 `VERSION` 为准。下文描述**产品能力**与**明确非目标**，便于对照部署与使用。

## 产品定位

RepoSentinel 面向**单用户私有部署**，集中值守 GitHub 仓库：

- 跨多个自有仓库查看 Issue、PR、Actions 与安全告警
- 以 GitHub App Webhook 为主要采集通道
- 重要事件实时通知，低优先级可进入摘要策略（见配置与规则）
- 可登记少量外部公开仓库

## 已交付能力

| 能力 | 说明 |
|------|------|
| 配置与主密钥 | 默认值 → YAML → 环境变量；AES-256-GCM 密钥环 |
| 双数据库 | Ent + Atlas；SQLite 默认，PostgreSQL 可选 |
| 唯一管理员 | 环境变量引导或本机 setup；CLI 重置密码 |
| Session / CSRF | HttpOnly Session、双提交 CSRF、登录限流 |
| Webhook 接收 | 验签（含 previous Secret）、Delivery 幂等、异步规范化 |
| 事件与资源 | Issue/PR、Workflow Run、三类安全告警、安装与仓库元数据 |
| 基线与乱序 | 新仓基线抑制通知；陈旧 `source_updated_at` 丢弃回滚 |
| 通知 | Outbox、Telegram、HTTP Webhook、死信重试、短时聚合与超频摘要 |
| 管理后台 | 仪表盘、仓库/基线、事件、投递、渠道配置、列表页、主题 |
| 运维 CLI | `doctor` / `backup` / `restore`、配置校验、密码重置 |
| 容器部署 | Dockerfile、Compose、GHCR 推送流水线、健康检查与 `/metrics` |

## 规划中 / 持续增强

下列能力在设计规格中已有约定，实现上可继续加深，**不阻塞**当前部署：

- 自有仓 GitHub API 周期对账与 Installation Token 刷新策略的完善
- 外部公开仓 Issues API 增量轮询与配额自适应
- 通知滑动窗口聚合与每日摘要调度的面板化配置

## 明确非目标

当前产品边界不包含：

- GitHub 个人通知收件箱同步
- 多用户注册、团队、RBAC 或多租户
- 在 Telegram 中写回 GitHub（关 Issue、合并 PR、处置告警等）
- 外部仓库的安全告警读取
- 超过 20 个外部公开仓库的大规模分布式轮询
- GitHub Enterprise Server 或自定义 GitHub Base URL
- Issue 评论、PR Review（approved / changes_requested）类事件

## 建议阅读

1. [快速开始](/guide/quick-start)
2. [Docker 部署](/deploy/docker)
3. [配置参考](/reference/configuration)
4. [能力与状态](/reference/implementation-status)

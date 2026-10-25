---
layout: home
title: RepoSentinel 文档
description: 自托管 GitHub 仓库动态与安全告警监控平台

hero:
  name: RepoSentinel
  text: 自托管的 GitHub 仓库值守
  tagline: Webhook · 安全告警 · 可靠通知 · SQLite / PostgreSQL
  image:
    src: /logo.svg
    alt: RepoSentinel
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/quick-start
    - theme: alt
      text: 功能介绍
      link: /features
    - theme: alt
      text: Docker 部署
      link: /deploy/docker
    - theme: alt
      text: GitHub
      link: https://github.com/Silentely/Repo-Sentinel

features:
  - icon: 📡
    title: 实时 Webhook
    details: Issue、PR、Actions 与三类安全告警及时入库；Delivery 幂等，乱序不回滚。
  - icon: 🛡️
    title: 基线与可靠落库
    details: 新安装仓库先建快照基线，避免历史通知洪流；指纹去重与陈旧写入保护。
  - icon: 📣
    title: 可靠通知
    details: Outbox 持久化；Telegram 与 HTTPS Webhook；失败重试与死信重试。
  - icon: 🎛️
    title: 值守仪表盘
    details: KPI、仓库与基线、最近事件与投递记录；暖调实用界面。
  - icon: 🔐
    title: 单用户安全基线
    details: 唯一管理员、Session/CSRF、主密钥加密凭据、敏感字段掩码。
  - icon: 🐳
    title: 容器友好
    details: 多阶段镜像与 Compose 样例；默认 SQLite 卷持久化。
---

## 界面展示

RepoSentinel 控制台采用独特的 **Retro Neo-Brutalism（复古新粗野主义）** 视觉设计语言：奶油纸张底色、粗黑墨水描边、零模糊硬阴影与胶囊型状态徽标，兼顾复古仪表盘的趣味与生产级控制台的高可读性。

### 监控控制台与核心仪表盘
::: tip 监控总览
仪表盘集中展示跨仓库核心 KPI 指标、Star 增长曲线、关键健康状态与近期重要事件流。
:::

![仪表盘预览](/images/dashboard-preview.png)

### 仓库列表与外部公开仓监控
::: tip 仓库管理
支持对自有组织仓库与外部关键开源依赖仓进行分级管理、增量同步与基线健康检查。
:::

![仓库列表预览](/images/repos-preview.png)

### Pull Requests 智能代码审查与合规裁决
::: tip AI 代码审查与敏感资产嗅探
PR 提交或更新时异步审查 Diff，给出健康评分、破坏性风险与维护者合入裁决（Ready to Merge / Block Risk 等），秒级嗅探敏感资产变动。
:::

![Pull Requests AI 审查预览](/images/pull-requests-ai-preview.png)

### 多渠道通知矩阵与聚合调度
::: tip 全渠道触达与防刷屏
支持 Telegram、飞书、企业微信、钉钉、Discord、Bark、Slack 与 HTTP Webhook，支持独立订阅类型勾选与免打扰静默时段。
:::

![通知渠道配置预览](/images/notifications-preview.png)

### 通知投递 Outbox 与实时交付排障
::: tip 投递可靠性保障
所有外发通知落库入 Outbox，提供完整的投递状态追踪、详情排障抽屉与死信一键重试。
:::

![投递记录预览](/images/outbox-preview.png)

### Webhook 负载检查与实时回放 (Inspector)
::: tip 零丢失可追溯
实时记录每一笔入站 GitHub Webhook Delivery，支持结构化元数据穿透检查与事件一键重放。
:::

![Webhook 检查与回放预览](/images/webhooks-preview.png)

### Star Release 公开仓库动态追踪
::: tip 依赖上游更新雷达
关注关键开源依赖的最新版本发布，ETag 条件轮询节省配额，支持 AI 中文提炼更新速览。
:::

![Star Release 追踪预览](/images/starred-releases-preview.png)

### 智能值守配置与系统偏好
::: tip 灵活配置
可自定义大模型端点、超时重试、运维时区、双因素认证（2FA）及仓库基线对账。
:::

![系统设置预览](/images/settings-preview.png)

### 移动端抽屉与响应式设计
::: tip 随时随地掌上运维
完美适配移动端视口，提供流畅的离屏抽屉导航与无溢出卡片式排版。
:::

![移动端控制台预览](/images/mobile-dashboard-preview.png)

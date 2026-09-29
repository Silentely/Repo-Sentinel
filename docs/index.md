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

### 智能值守配置与系统安全
::: tip 灵活配置
可自定义 LLM 审查提示词、通知聚合策略、双因素认证（2FA）及多渠道推送矩阵。
:::

![系统设置预览](/images/settings-preview.png)

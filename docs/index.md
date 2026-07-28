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
    details: Issue、PR、Actions 与三类安全告警即时入库；Delivery 幂等，乱序不回滚。
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

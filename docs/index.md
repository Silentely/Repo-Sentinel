---
layout: home
title: RepoSentinel 文档
description: 自托管 GitHub 仓库动态与安全告警监控平台

hero:
  name: RepoSentinel
  text: 自托管的 GitHub 仓库值守
  tagline: Webhook · API 对账 · 安全告警 · Telegram 通知 · SQLite / PostgreSQL
  image:
    src: /logo.svg
    alt: RepoSentinel
  actions:
    - theme: brand
      text: 快速开始
      link: /guide/quick-start
    - theme: alt
      text: 功能与路线图
      link: /features
    - theme: alt
      text: 实现状态
      link: /reference/implementation-status
    - theme: alt
      text: GitHub
      link: https://github.com/Silentely/Repo-Sentinel

features:
  - icon: 🔐
    title: Phase 1 已就绪
    details: 配置与主密钥、双数据库迁移、唯一管理员、Session/CSRF、健康检查与 CLI 恢复。
  - icon: 📡
    title: 实时 Webhook（规划中）
    details: issues / PR / workflow_run / 三类安全告警；首次安装只建基线，避免通知洪流。
  - icon: 🔄
    title: 对账与外部仓（规划中）
    details: 自有仓定期对账；最多 20 个外部公开仓增量轮询，配额自适应。
  - icon: 📣
    title: 可靠通知（规划中）
    details: Outbox + Telegram / HTTP Webhook；聚合抑制风暴；每日摘要可配置时刻。
  - icon: 🎛️
    title: 值守仪表盘（规划中）
    details: 暖调实用 UI：健康胶囊、需要关注列表、空状态上手路径。
  - icon: 🐳
    title: 单容器部署（规划中）
    details: 多阶段镜像、Compose 安全基线；当前可从源码本地运行验证 Phase 1。
---

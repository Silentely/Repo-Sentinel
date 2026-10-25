# Repo-Sentinel Phase 2 实施计划：智能化与决策情报深化

## 阶段目标
本阶段将 Repo-Sentinel 从“被动事件通知者”升级为“主动决策支持与韧性值守平台”，全量落地 Phase 2 规划的 5 项核心能力：
1. **智能 Release 破坏性变更与本地依赖影响雷达 (Dependency & Breaking Radar)**
2. **动态仓库健康评分与僵尸工单停滞哨兵 (Stagnant Sentry & Health Scorecard)**
3. **Actions CI 偶发失败与故障归因聚类分析 (Flaky CI & Failure Clustering)**
4. **多通知渠道自动故障转移与告警升级机制 (Multi-Channel Failover & Escalation)**
5. **轻量进程外插件基座契约与生命周期管理器 (Out-of-Process Plugin Provider Prototype)**

## 架构原则
- **保持 Go 极简模块化单体架构**：零强制外部重型中间件依赖，SQLite / Postgres 双引擎兼容。
- **类型安全契约与真实改坏变红测试**：所有领域逻辑均配备确定性单元测试，改坏实现必须能触发测试失败。
- **双端一致与渐进式展示**：后端 API 支撑前端 React SPA 呈现直观交互组件（徽章、诊断、影响雷达、归因卡片）。

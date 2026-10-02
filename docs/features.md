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
| 事件与资源 | Issue/PR、Star/Watch、Workflow Run、三类安全告警、安装与仓库元数据 |
| 仓库能力开关 | 单仓独立开关：监控（总开关）、Issues、PR、Star/Watch、Actions、安全告警；关闭即停止采集、不建事件、不通知；归档联动关闭全部开关 |
| 列表筛选与忽略 | 按仓库筛选；本地忽略长期打开项（不回写 GitHub）；事件流/每日汇总与资源列表默认排除归档仓与已忽略项 |
| 基线与乱序 | 新仓基线抑制通知；陈旧 `source_updated_at` 丢弃回滚 |
| 通知 | Outbox、多渠道（Telegram、飞书、企微、钉钉、Discord、Bark、Slack、HTTP Webhook）、死信重试、短时聚合与超频摘要 |
| 渠道订阅 | 每渠道独立勾选订阅类型（Issue、PR、Star/Watch、Actions、Dependabot、Code Scanning、Secret Scanning，默认全部）与「定期汇总」开关，日/周/月报告可分别订阅（默认开），合并通知按订阅重建子集 |
| 免打扰时段 | 渠道级静默时段（HH:MM 起止 + IANA 时区，默认 22:00–08:00 UTC），静默期内常规事件延迟到恢复时刻投递；Secret Scanning、Critical/High 级 Code Scanning 与 Dependabot 告警固定穿透即时送达 |
| 全局功能模块 | Issues / PR / Actions / 安全告警 / Star / Watch / Star Release：关闭后停止采集、对账与实时/摘要通知；Issues/PR/Actions/安全告警关闭同时隐藏侧栏入口，Star 关闭同时隐藏仪表盘 Star 增长面板；仓库级开关在全局关闭时禁用 |
| Star Release 追踪 | 匿名枚举指定用户公开 star 仓库（自动排除 fork/archived），ETag 条件请求轮询各仓最新 Release；新版本实时通知，可配 AI 中文总结；500 追踪上限、双周期可配置、unstar 自动停用、独立 `/starred-releases` 管理页 |
| 管理后台 | 仪表盘（含 Star 增长曲线）、仓库管理、Issues/PR/Actions（含 CI 效能与耗时洞察）/安全告警、Webhook 检查与历史回放、Star Release 追踪、渠道配置、主题 |
| 离线可用 | 管理后台注册 Service Worker：导航请求网络优先、断网回退应用外壳，静态资源本地缓存，弱网/断网仍可打开页面 |
| 运维 CLI | `doctor` / `backup` / `restore`、配置校验、密码重置 |
| 容器部署 | GHCR 镜像（`latest` 随正式 tag）、Compose 拉取部署、健康检查与 `/metrics` |
| 历史数据保留 | 事件 / 终态投递 / Webhook Delivery 可配置保留天数，后台定期清理（0 禁用） |
| PR AI 代码审查 | PR 开启/更新异步 Diff 审查、健康评分、识别安全与破坏性风险；支持敏感关键资产变动嗅探（CI/CD 工作流、依赖清单、DB 迁移、环境凭据）与维护者合并裁决指引（Ready to Merge / Needs Tests / Needs Manual Review / Block Risk）；管理后台支持「立即审查/重新审查」（异步入队 + 自动轮询结果）与一键复制 Markdown 报告；高危风险自动联动 Outbox 发送多渠道安全预警；支持 GitHub 评论回写与 MCP 工具集成 |
| Issue 智能分诊与首响应 | 新 Issue 创建自动意图分类、优先级评估、排查要素完整度审计（复现步骤/环境/堆栈日志等缺失项提取），生成专业得体的维护者首响应草稿；管理后台支持查看、立即/重新分诊、一键复制建议首响应及跳转 GitHub 快速回复 |
| Actions CI 失败智能诊断 | 捕获 Workflow 失败运行与具体失败 Job/Step，LLM 自动分析故障原因并生成修复建议附加于通知正文；独立开关（`failure_analysis_enabled`）与安全告警分诊互不影响，失败步骤输入有上限防超长输入 |
| 实时 SSE 推流与前端防抖 | 服务端轻量广播总线（`/api/v1/events/stream`），前端 150ms 窗口防抖合并，局部失效 TanStack Query 缓存，内置指数退避重连与熔断降级 |
| SQLite 维护解耦与探针隔离 | 独立单连接短超时（1000ms）带抖动执行 `PRAGMA wal_checkpoint(PASSIVE)` 与 `optimize`；`/health/ready` 接入 1.5s 隔离探针，高负载下就绪检测不挂死 |
| ChatOps 回调与防重放 Token | 支持 Telegram 与飞书按钮交互回调；基于 128 位 ULID 事务原子单次消费 Action Token（唯一约束保证跨进程仅一个消费者），Telegram 紧凑化适配，多层级安全验签；令牌与领取标记随保留清理节拍按 TTL 清除 |
| AI Issue 自动打标防御 | 基于白名单映射规范化标签（`sentinel:*`），严苛过滤垃圾分类；GitHub API 422 容错与系统级设置开关 `ai.auto_label_enabled`；打标回执键原子竞争防并发重复打标，失败即释放可重试，回执 90 天后随临时设置清理 |
| 工作项批量忽略与防倒流守卫 | 单事务批量更新忽略标记（上限 100 条，任一 ID 缺失整体回滚并返回 404）；closed 终态防陈旧消息倒流；前端 BatchActionBar 悬浮操作栏 |
| 对账时效遥测与态势胶囊 | 统计活跃仓最大同步滞后时间（`max_lag_seconds`）与异常状态；Prometheus 导出 `reposentinel_sync_max_lag_seconds`；仪表盘态势胶囊（健康/滞后/异常） |
| 机器账号识别与渠道降噪 | 统一 `botutil` 判定模块；区分作者与触发者 Bot 属性；通知渠道支持 Issue/PR 机器账号免打扰（安全告警除外）；前端 `[Bot]` 徽标 |
| 前缀搜索与分诊收件箱 | 支持 `is:open`、`is:pr`、`author:xxx`、`is:bot` 前缀搜索语法并与下拉框双向联动；收件箱（Inbox）/ 已归档（Archived）分诊流与真人操作受控自动唤醒 |
| 全局指令面板与键盘流 | `Cmd+K` / `Ctrl+K` 快速唤起全局指令面板；`G D`/`G I`/`G P`/`G R`/`G A`/`G S`/`G N` 快捷跳转；破坏性动作强制二次确认守卫 |
| 复合游标审计检索与写入期脱敏 | `(created_at, id)` 复合游标消除深分页扫描开销；审计写入期深度递归脱敏机密凭据（password/token/secret/api_key） |

## 可持续增强

下列能力可继续加深，**不阻塞**当前部署：

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

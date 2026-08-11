# Changelog

本文件遵循 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)，版本号遵循 SemVer。

## [Unreleased]

### Changed

- Star 增长曲线 Y 轴不再从 0 起：围绕数据波动范围自适应缩放（大基数下个位增长肉眼可见），波动大时贴近「上下 100」的常规观感
- 关于页「构建时间」恢复绝对日期展示（不再显示「X 天前」相对时间）；移除 Git SHA 复制按钮，直接展示 SHA 文本
- 仓库「彻底删除」失败后按钮恢复并提供原因提示（此前失败会永久停留在「删除中…」）
- 设置页保存某一区块不再覆盖另一区块尚未保存的编辑；各区块保存错误独立展示（此前任一区块失败会在两处同时报错）
- 投递记录页渠道筛选、Star Release 追踪页分页与状态筛选同步到 URL：刷新 / 复制链接后保留当前视角
- 仪表盘与投递记录页单条重试失败时给出原因提示（此前静默恢复，仅错误码 hover 可见）
- 聚合通知标题「（已合并）」改为「（已聚合）」：避免与 PR「已合并」状态语义混淆
- 定期报告空事件文案由庆祝 emoji 改为 📭；分组标题与实时通知的类型/状态文案统一（同一映射，不再各自维护）
- 摘要预览补齐 PR 转草稿、Star/Watch/Release 状态中文（此前回退为裸英文 action）
- Star Release 追踪行停用/恢复的忙碌态随请求结束自动恢复
- 通知渠道目标 / 投递 ID / Webhook URL 复制失败时给出短暂提示（此前静默降级）

### Added

- 可访问性：忽略筛选按钮补 `aria-pressed`；移动端导航抽屉与投递详情抽屉补 Tab 焦点循环与模态语义；移动端主题选择器不再聚焦到不可见控件
- 状态徽章补全 `action_required` / `skipped` 与 release / star / watch 类型配色，深色主题下自动提亮
- 事件状态徽章与按钮选中态改用设计令牌（含深色档），主题色从令牌读取：改配色不再需要同步多处硬编码
- 列表页底部「已全部加载」处新增「回到顶部」按钮，长列表滚动后一键回顶
- 对账与外部轮询单仓成功留痕（Debug，`reconcile ok` / `external poll ok`）：排查"某仓对账过没有"不依赖调度成功日志
- 登录成功日志补充 `user_agent`：登录来源可审计
- 仓库生命周期事件（归档/取消归档/删除/转移）Info 留痕
- Webhook 状态标记失败（`mark_failed`）Warn 留痕：标记失败会让行残留中间态，影响状态机与重放判断
- 定期报告生成了但没有启用且勾选汇总的渠道时 Debug 留痕（用户看不到报告是常见困惑点）
- GitHub REST 出站请求补充 `User-Agent: RepoSentinel-GitHubClient/1.0`（GitHub 要求 UA 识别来源）
- JSON 请求体超过 1 MiB 上限时响应携带具体说明（不再只给通用校验文案）
- 可访问性：列表/outbox/仓库归档筛选按钮补 `aria-pressed`；对账按钮补 `aria-busy`；dashboard 错误码 hover 展示中文说明（与投递记录页一致）；多处新窗口链接补 title 提示
- Star 增长曲线在深色主题下适配设计令牌：Tooltip 背景/边框/文字与刻度颜色随主题切换，不再白底刺眼
- 全局滚动条样式适配深/浅主题（WebKit），深色下滚动条不再刺眼
- `/metrics` 新增 `reposentinel_outbox_pending_gauge` 与 `reposentinel_outbox_sending_gauge`：投递队列深度与在途量可监控
- FAQ 补充「收不到通知怎么排查」：按链路逐段确认，并给出 Debug 日志关键词
- 通知决策留痕：实时通知被抑制 / 能力开关关闭 / 不在实时范围时 Debug 输出原因，排查漏通知不再盲猜
- 聚合器超频降级 Warn 留痕（repo/窗口内事件数）与合并投递 Debug 留痕（合并量可评估聚合窗口配置）
- Webhook 处理成功日志补充 `event_id`：delivery 行 ↔ 事件可互相检索定位
- Outbox 详情抽屉展示通知正文（纯文本化，外部 HTML 不直接渲染避免注入）
- 通知渠道行支持一键复制目标（Chat ID / URL），配置排查时便于粘贴
- `reposentinel version` 输出补充 `repository=` 仓库地址
- 通知渠道目标失焦即时校验：Telegram Chat ID 须为数字、HTTP Webhook 仅接受 HTTPS URL，保存前提前反馈格式问题
- 登录被限流（`rate_limited`）后提交按钮禁用，防止连点刷掉限流窗口
- Outbox 详情抽屉支持一键复制投递 ID，便于粘贴到日志/工单排查
- Session 清理（15m 周期）无论删除量都 Debug 留痕：排查"过期 Session 有没有清"不依赖删除数
- 事件去重留痕：Webhook 重复送达（指纹预查命中与唯一索引冲突）Debug 输出 repo/kind/action，与「逻辑异常没写库」区分
- `installation_repositories` 的 `repositories_removed` 消费留痕：本地存在标记 unavailable、本地不存在也 Debug 记录（确认事件被消费而非漏处理）
- 设置页「立即对账全部自有仓」无自有仓时禁用并提示原因
- 仓库地址入口：登录页/初始化页右上角与顶栏新增 GitHub 图标直达源码（lucide-react 无品牌图标，内联 octocat SVG），关于页「你在用什么」补「GitHub 仓库」链接，`/auth.md` 元数据补 `- GitHub:` 行
- HTTPS 部署（PublicBaseURL 为 https）下发 `Strict-Transport-Security`；明文部署不下发，避免锁死纯 HTTP 自托管
- CSP 增强：新增 `form-action 'self'`，HTTPS 部署追加 `upgrade-insecure-requests`
- `reposentinel healthcheck` 成功输出补充 `latency_ms`：编排系统可发现"能响应但明显变慢"的实例
- 列表页翻页失败时底部展示「加载失败，点击重试」（首屏失败仍由 QueryGate 兜底）
- 登录凭据失败后自动清空密码框并聚焦：避免旧输入残留被误提交
- HTTP Webhook 出站投递携带明确 `User-Agent: RepoSentinel-Webhook/1.0`，接收端日志/过滤可识别来源
- 设置页时区输入失焦即时校验：非法 IANA 时区提前提示，保存后后端仍强校验
- 采集跳过留痕：Webhook 规范化对"收到事件但没写数据"的静默路径输出 Debug 日志并带原因（`feature_disabled` / `monitor_off` / `archived_or_unavailable` / `capability_off`），排查开关与仓库状态变化不再盲猜
- 版本检查（updatecheck）各路径 Debug 留痕：缓存命中 / 成功（含来源与版本）/ 回退过期缓存 / 最终失败
- Webhook 单条处理耗时超过 5s 时 Warn 留痕（`webhook_slow`，含 delivery/event/repo/duration）：数据库抖动或外部调用阻塞一目了然
- Star 增长曲线 tooltip 展示当日增长量（较前一日 +N / -N），首日无参照只显示总数
- 三个列表页（Issues/PR、Actions、安全告警）筛选后为空时，空态提供「清除筛选」按钮直达无筛选视图
- 路由错误兜底页新增「返回仪表盘」快捷链接
- Outbox「重试全部失败」为批量操作增加确认对话框，防误触
- 主题切换时画布与文字色平滑过渡，深/浅色不再生硬跳变
- 渠道「发送测试通知」正文带发送时刻（UTC），多条测试通知可区分收到的具体是哪一条
- 登录失败日志补充 `username`（不含密码）与 CSRF 校验失败日志补充来源信息：暴力尝试与写请求被拒可审计
- Webhook 处理成功日志补充 `stale_discarded` / `unhandled_action` 布尔：乱序丢弃等"处理了但没通知"的原因可直接从日志识别
- 三个列表页（Issues/PR、Actions、安全告警）筛选激活时提供「清除筛选」按钮，一键回到无筛选视图
- 仓库管理页归档视图同步到 URL（`?archived=1`），刷新后保留当前视角
- Outbox 状态筛选同步到 URL，仪表盘「投递失败」指标跳转直达 `?status=dead` 筛选
- 关于页 Git SHA 支持一键复制并反馈「已复制」
- 登录页对 `rate_limited` 给出限流说明（按来源 IP 生效，换用户名不重置额度）
- 列表页（Issues/PR、Actions、安全告警）筛选条件同步到 URL：刷新或复制链接后保留当前筛选；新增 `useUrlState` 共享 hook 与受限枚举解析
- 设置页成功提示（偏好/功能开关/AI 配置/密码）3 秒后自动消退，连续提交会重置计时，避免多条成功信息常驻堆叠
- 登录页载入后用户名输入框自动聚焦，减少一次点击即可开始输入
- Outbox 投递记录页新增「重试全部失败」：跨页收集全部 dead 投递并逐个重新排队，用于失败记录跨多页时一键恢复；按钮仅在有失败投递时显示
- Webhook 处理失败计数指标（`reposentinel_webhook_failed_total`）：规范化或规则评估失败时递增，与 WebhookDelivery 的 failed 状态对应
- 管理台按路由更新浏览器标签页标题（「仪表盘 · RepoSentinel」等）：多标签场景可直接看出当前页面；登录与初始化页设置独立标题
- 渠道订阅类型提供「全选 / 清空」快捷操作与已选计数，全选仅勾选全局开关未关闭的类型
- 投递记录详情抽屉对已收录的错误码展示中文排障提示（如 Telegram 限流、Chat ID 无效、接收端 5xx），机器码保留便于对照日志
- 列表与仪表盘的相对时间（「X 分钟前」等）hover 显示精确绝对时间，统一收敛为 `RelativeTime` 组件
- 侧边栏「关于与设置」拆分为「关于」与「设置」两个入口：关于页聚焦版本信息、更新检查与运维提示；设置页承载运行偏好、功能模块、AI 集成与账号管理
- 仪表盘的「仓库与基线对账」面板迁入设置页（`/settings`），对账、单仓放行与基线状态集中在设置页维护；仪表盘保留接入进度与关键指标
- 仓库管理页支持「彻底删除」：`DELETE /api/v1/repositories/{id}` 级联清理该仓库全部本地数据（PR/Issue、事件、告警、快照、游标、待投递通知），用于 GitHub 侧仓库已删除但 `repository.deleted` webhook 漏投递时的手动收口
- 仓库级联删除：GitHub 侧删除仓库（`repository.deleted` webhook）时，自动清理本地仓库与全部关联数据（PR/Issue、事件、告警、star 快照、同步游标、待投递通知），不留孤儿数据
- AI 集成配置新增「测试连通性」：以当前生效配置发送一次最小对话验证端点 / 模型 / API Key，返回耗时与结果；未锁定字段可在请求中临时覆盖（保存前验证），不写库、不改变运行时
- SPA 静态资源（HTML / JS / CSS / JSON / SVG）按客户端能力 gzip 传输，降低自托管出站带宽；带 Range 的请求与非文本类型不压缩
- 访问日志（debug 级）补充 `user_agent` 字段，便于区分浏览器、Agent 客户端与爬虫流量
- Telegram 通知发送前按 4000 字符上限安全截断：按 Unicode 码点截断避免乱码，截断点落在 HTML 标签/实体中间时回退到完整位置，超长消息不再因 400 进入死信

### Changed

- 历史数据清理（retention）无过期数据时也留痕（Debug，含删除量与保留策略）：排查"清理到底跑没跑"不依赖删除量
- Workflow 结论中文标签（成功/失败/超时/…）收敛到 store 领域层 `WorkflowConclusionLabel`：rules 实时通知与 digest 定期报告共用同一映射，消除两处维护漂移；emoji 逻辑同步收拢
- 调度器成功执行留痕为 Debug 级（task + duration_ms）：正常周期不刷屏，排查「任务有没有跑」时开 debug 即可确认
- Outbox 每 tick 领取批量由 20 提升到 50（`claimBatchSize`）：突发积压（如 GitHub 批量推送）时单轮消化更多，避免队列长期堆积
- Webhook 未配置 Secret 时 503 响应补 `Retry-After: 60`：GitHub 按 5xx 退避重试，配置未就绪期间不再高频重试
- 每日/每周/月度报告正文末尾补「生成时间」页脚（UTC，与规则通知时间格式一致），便于判断报告新鲜度
- Webhook 拒绝日志（未配置 Secret / 验签失败）补充 `delivery_id` 与 `event_type`：GitHub 投递失败可按投递 ID 在两侧日志间交叉定位
- Webhook 后台处理增加并发限流（信号量，默认 32）：突发事件不会无限起 goroutine 同时写库/调 GitHub API，超出部分排队等待而非丢弃；实例关闭期间不再排队新工作
- Webhook 重复投递的日志与 202 应答收敛为单一函数，两个命中分支行为保持一致
- 管理台查询失败提示支持原地重试；投递记录页的详情/重试控件遵循可访问交互，批量重试明确反馈成功与失败数量
- 通知渠道的测试、启停、删除状态按 Telegram / HTTP Webhook 独立展示，操作结果文案带出具体渠道名称
- HTTP Webhook 与 AI 上游错误详情设置读取上限并标记截断；AI 成功响应超出 1 MiB 时拒绝解析，避免异常响应消耗过多资源
- 调度失败日志补充稳定的任务名与耗时字段，便于定位慢任务和区分不同报告周期

### Fixed

- `useUrlState` 筛选状态此前只写 URL 不更新本地 state（点击筛选不生效）：改为本地 state 与 URL 双向一致，set 立即触发重渲染
- 自有仓对账遇 404/410（仓库已删除或不可见）不再每轮反复失败：兜底标记「不可用」并暂停采集，与外部仓轮询降级语义一致（webhook `repository.deleted` 漏投递时靠此收口）
- `installation_repositories` 的 `repositories_removed` 事件此前未解析，仓库被移出安装（授权收回）后仍持续对账失败；现解析并标记「不可用」，等待重新授权后恢复
- 主题预置脚本由 index.html 内联改为同源外部脚本（`/theme-init.js`）：全局 CSP `default-src 'self'` 会拦截内联脚本，原实现在深色主题下首帧仍闪浅色，主题预置实际未生效
- 两处空态操作行（仪表盘与仓库列表）的内联 `style` 属性被 CSP `style-src 'self'` 拦截而静默失效，改为 `link-row--centered` CSS 类
- 相对时间格式化对"未来时间"（客户端与服务端时钟偏差、计划事件）不再渲染空白，统一归为「刚刚」；超过 30 天改用「X 个月前 / X 年前」粒度，与列表其余行的相对时间风格一致
- AI 集成配置：API Base URL / 模型 / 请求超时 / 输出 token 上限此前因配置层注入默认值而被误判为环境变量锁定，管理台无法编辑且保存返回 `ai_field_locked`；现仅当通过 `REPOSENTINEL_AI_*` 或 YAML 显式设置时锁定，未设置字段可在管理台编辑并持久化（实际取值仍由 AI 客户端在使用点回退默认）
- AI 连通性测试：探测请求改用按配置超时的专用 HTTP 客户端（不再被包级 30s 硬顶截断），前端测试请求放宽至 60s 等待；AI 配置查询失败时禁用保存 / 测试 / 清除按钮，防止表单默认值覆盖数据库中的有效配置
- AI 连通性测试：探测时长设 15s 上限并返回友好超时提示，避免同步探测拖过 HTTP Server WriteTimeout（30s）导致反向代理报 `connection termination` 而看不到真实错误；非 2xx 响应正文（如网关错误说明）纳入错误信息，前端对非 JSON 错误回退展示截断原文，不再吞掉真实原因
- AI 区块操作按钮（保存 / 测试 / 清除）改为与通知渠道一致的 `channel-form__buttons` 容器布局，间距与对齐统一
- 示例配置 `configs/reposentinel.example.yaml` 的 AI 标量字段改为注释展示，并说明显式设置会在管理台锁定，避免复制即用的部署再次误锁
- AI 启动校验与管理台校验对齐：`ai.base_url` 同样拒绝 URL 内嵌凭据（userinfo）

## [0.3.8] - 2026-08-05

### Added

- 可选 AI 集成（默认关闭）：OpenAI 兼容 Chat Completions 客户端，可通过 `REPOSENTINEL_AI_*` 配置，支持接入本地模型（Ollama 等）；AI 不可用时自动降级，不影响通知投递
- 每日摘要 / 周报 / 月报正文由 LLM 生成自然语言总结（`ai.digest_enabled`），失败回退原模板
- 实时安全告警（Dependabot / Code Scanning / Secret Scanning 新告警）通知附带 AI 影响分析与处理建议（`ai.triage_enabled`）
- 每周 / 每月定期报告（默认关闭）：发送日与发送时刻经 `report.*` 系统设置配置，正文复用汇总模板并支持 AI 总结
- settings API 新增 `report.weekly_enabled` / `report.weekly_day` / `report.monthly_enabled` / `report.monthly_day` 键
- AI 配置可在管理台「关于与设置 → AI 集成」编辑：环境变量已设置字段在管理台锁定，API Key 经主密钥加密存库且不回显，保存后热生效无需重启

### Changed

- 通知渠道「接收每日汇总」文案更新为「接收定期汇总（日/周/月）」，勾选后同时接收每日、每周、每月报告

## [0.3.7] - 2026-07-30

### Added

- 历史数据保留策略：事件 / 终态投递 / Webhook Delivery 保留天数可在「关于与设置」配置（默认 90 / 30 / 30 天，0 表示禁用该类清理），后台每日自动清理
- Issues / PR / Actions / 安全告警列表抽取共享骨架屏与功能开关守卫组件，加载中展示列表骨架而非空白

### Fixed

- 全局功能模块开关（Issues / PR / Actions / 安全告警）此前仅隐藏 UI，现同时拦截 Webhook 采集、对账同步与实时/摘要通知；仓库管理页对应能力在全局关闭时显示为关且不可改
- 聚合窗口期间关闭全局功能开关时，已入桶事件在 flush 时按最新开关过滤，不再漏发合并通知
- 应用装配失败路径未取消 worker 上下文导致的 goroutine 泄漏（`go vet` 已拦截）
- 对账期间 issues / PR 全局功能开关全关时不再推进 issues 增量游标，重新开启后仍可拉回关窗期内的变更
- 通知文案 HTML 转义统一为标准库 `html.EscapeString`（含单引号），修复合并消息与实时消息转义行为不一致
- GitHub API 剩余配额头缺失或解析失败时不再误报「配额低」日志（原先剩余 0 会误触发）
- 仓库 / Actions / 事件 / 安全告警 / 投递记录列表排序补 ID 次级键，批量同步产生相同时间戳时分页不再错位
- HTTP Webhook 通道解析 429/503 响应的 `Retry-After` 响应头（秒或 HTTP 日期），按上游指引退避而非固定阶梯
- GitHub 429 / 配额耗尽 403 返回携带上游建议等待时长的限流错误（`Retry-After` / `X-RateLimit-Reset`），token 签发端点同样归类
- 对账与外部轮询遇限流停止本轮（等待时长在 2 分钟预算内先等待再继续），不再逐仓连环请求放大次限流
- Actions 恢复事件此前永不触发：`LatestCompleted` 查询位于写入之后命中当前运行自身，现前移为写入前基线查询，失败后成功会正确标记 `recovered`
- Installation Token 并发获取合并为单次签发请求（single-flight），Webhook 处理与后台同步并发触发时不再对 GitHub 签发端点惊群请求

### Changed

- 管理台体验：侧栏按全局功能隐藏入口；死信角标改挂「投递记录」；仪表盘 KPI 可点钻取并随功能开关显隐；接入进度条；渠道 target 回填且空 target 不覆盖；关于页分块保存并暴露超频窗口；仓库开关层级与归档确认；GitHub 页入站/出站双状态灯
- Telegram 合并通知与每日汇总的 Actions 类别标签由「工作流」统一为「Actions」，与前端一致

## [0.3.6] - 2026-07-29

### Added

- 列表本地忽略：Issue / PR / Actions / 安全告警可标记忽略（不回写 GitHub），支持「关注中 / 已忽略」切换
- Issues / PR / Actions / 安全告警页增加按仓库筛选
- 忽略 API：`PATCH /api/v1/work-items/{id}/ignored`、`PATCH /api/v1/workflow-runs/{id}/ignored`、`PATCH /api/v1/security-alerts/{id}/ignored`
- 列表查询参数 `ignored=true|all`；默认排除已忽略项
- 数据库迁移 `20260729000300_item_ignored`（PostgreSQL + SQLite）
- 仪表盘区块（通知投递 / 最近事件 / 仓库与基线）支持折叠，状态写入 localStorage
- 系统设置增加 Issues / PR / Actions / 安全告警全局功能模块开关
- CI 构建工作流完成后自动清理 3 天前的历史运行记录（保留最近 3 条）
- 投递记录页面（`/notifications/outbox`）
- 开发规范文件 `CLAUDE.md`

### Fixed

- 侧边栏导航徽章数字被半透明规则污染导致看不清
- 已归档仓库的历史 Issue / PR / Actions / 告警仍出现在列表与侧栏计数中
- 仪表盘「仓库与基线」仍展示已归档仓库；动作按钮列不对齐
- Actions 空状态未说明权限/事件/对账排查路径；请求失败时静默空白
- 仪表盘 `open_issues` 计数误含 open PR
- PostgreSQL `workflow_runs` 表 `github_run_id` / `github_workflow_id` 为 int4，GitHub run_id 溢出导致 Actions 数据全部入库失败
- 侧边栏点击「投递记录」时「渠道配置」同时高亮（前缀匹配未精确）
- 仓库下拉筛选仍显示已归档仓库
- PR 页面审核/检查筛选按钮过多导致布局拥挤

### Changed

- 列表与仪表盘统计默认排除已归档仓库与已忽略项
- 仪表盘区块顺序调整为：通知投递 → 最近事件 → 仓库与基线
- 长标题单行截断、事件/投递行改为主内容 + 右侧动作布局
- 「死信」文案统一改为「投递失败」
- PR 页面审核状态与检查状态筛选改为下拉选择器

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
- `PATCH /api/v1/repositories/{id}/settings` API 端点
- 数据库迁移 `20260729000100_repo_capability_toggles`（PostgreSQL + SQLite）

### Fixed

- `workflow_run` Webhook 处理时 GitHub 偶发缺字段导致数据库写入失败
- `mapStoreError` 吞掉原始错误信息，现在保留完整错误链便于排障
- `handleActivateRepository` 未检查 Upsert 返回错误
- 侧边栏随主内容滚动，改为固定定位

### Changed

- `Upsert` 不再覆盖用户配置的仓库能力开关，能力开关仅通过 `UpdateSettings` 修改

## [0.3.4] - 2026-07-28

### Fixed

- 文档与工作流描述与上述标签规则一致

### Changed

- GHCR 标签：`main` → `main` + `main-<sha>`；`dev` → `dev` + `dev-<sha>`；正式 `v*` → `vX.Y.Z` + `latest`（双架构）
- 补充项目协作文档：CONTRIBUTING、SECURITY、PR / Issue 模板、Dependabot、发布说明

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

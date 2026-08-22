# 列表 API 查询参数

监控类列表端点共享分页契约：`page`（默认 1）、`per_page`（默认 20，上限 100），响应统一为 `{ items, page, per_page, total }`。本页列出各端点的专属过滤参数与取值约定。

## GET /api/v1/work-items

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `kind` | `issue` / `pull_request` | 工作项类型 |
| `state` | `open` / `closed` | `state=closed` 时默认按 `display.closed_limit` 设置限量 |
| `repository_id` | 仓库 ID | 限定单仓 |
| `review` | `approved` / `changes_requested` / `pending` | PR 审核结论；`pending` 映射为尚无审核记录（服务端存空串） |
| `check` | `passed` / `failed` / `pending` | PR 检查状态；`passed`/`failed` 分别映射存储值 `success`/`failure`，`pending` 匹配空串或 `pending` |
| `ignored` | `true` / `all` | 默认仅返回未忽略项；`true` 仅已忽略，`all` 含全部 |

`review` / `check` 的组合过滤在 SQL 层完成，分页与 `total` 均为过滤后结果。

## GET /api/v1/notifications/outbox

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `status` | `pending` / `sending` / `sent` / `dead` | 通知投递状态（重试中的条目仍为 `pending`，由 `attempt_count` 体现） |
| `channel_type` | 渠道类型（如 `telegram`） | 服务端解析为渠道 ID 集合后下推 SQL；无匹配渠道时返回空页且分页参数已归一化 |

## GET /api/v1/repositories

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `type` | `installation` / `external` | 仓库来源类型 |

## GET /api/v1/events

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `kind` | 事件类型 | 如 `issue`、`pull_request`、`workflow_run` |
| `repository_id` | 仓库 ID | 限定单仓 |

## GET /api/v1/workflow-runs

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `conclusion` | workflow 结论（如 `success`、`failure`） | 按 `conclusion` 列过滤 |
| `repository_id` | 仓库 ID | 限定单仓 |

## GET /api/v1/security-alerts

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `alert_kind` | `dependabot` / `code_scanning` / `secret_scanning` | 告警来源 |
| `state` | `open` / `dismissed` / `fixed` / `withdrawn` | 告警状态；`withdrawn`（已撤回）由对账检测到源端告警消失后写入 |

## GET /api/v1/starred-releases/trackers

| 参数 | 取值 | 说明 |
| --- | --- | --- |
| `state` | `tracking` / `inactive` / `disabled` / `unavailable` | Star Release 追踪状态；`inactive`（无 Release）默认 7 天复查，`disabled` 为手动停用 |

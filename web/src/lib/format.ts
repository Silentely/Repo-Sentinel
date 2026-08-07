/**
 * 将 ISO 日期字符串格式化为相对时间（中文）。
 * 用于列表与仪表盘共用。
 * @param now 可选基准时间（测试注入用），默认当前时间。
 */
export function formatRelativeTime(dateString: string, now: Date = new Date()): string {
  if (!dateString) return "";
  const date = new Date(dateString);
  if (isNaN(date.getTime())) return "";
  const diffMs = now.getTime() - date.getTime();
  // 未来时间（客户端与服务端存在时钟偏差、或事件带计划时间）不渲染空白，
  // 与 60 秒内同样归为「刚刚」，避免列表时间列留白。
  if (diffMs < 60 * 1000) return "刚刚";
  const diffSeconds = Math.floor(diffMs / 1000);
  const diffMinutes = Math.floor(diffSeconds / 60);
  const diffHours = Math.floor(diffMinutes / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffMinutes < 60) return `${diffMinutes} 分钟前`;
  if (diffHours < 24) return `${diffHours} 小时前`;
  if (diffDays < 30) return `${diffDays} 天前`;
  // 超过一个月改用月/年粒度，与列表其余行的相对时间风格保持一致；
  // 直接显示绝对日期会与整列「X 前」的节奏割裂。
  if (diffDays < 365) return `${Math.floor(diffDays / 30)} 个月前`;
  return `${Math.floor(diffDays / 365)} 年前`;
}

/** 仓库同步状态 → 中文展示文案（仪表盘与列表页共用，避免两处维护漂移）。 */
export function syncStatusLabel(status: string): string {
  switch (status) {
    case "baseline_sync":
      return "基线中";
    case "active":
      return "正常";
    case "archived":
      return "已归档";
    case "unavailable":
      return "不可用";
    default:
      return status;
  }
}

/** 仓库显示名：full_name 缺省时回退 owner/name 拼接。 */
export function repoDisplayName(repo: { full_name?: string; owner?: string; name?: string }): string {
  return repo.full_name || (repo.owner && repo.name ? `${repo.owner}/${repo.name}` : repo.name || "");
}

/** 安全告警类型 → 中文展示文案。 */
export function alertKindLabel(kind?: string): string {
  switch (kind) {
    case "dependabot":
      return "依赖漏洞";
    case "code_scanning":
      return "代码扫描";
    case "secret_scanning":
      return "密钥泄露";
    default:
      return kind || "告警";
  }
}

/** Outbox 投递状态 → 中文展示文案（仪表盘与投递记录页共用，消除同枚举中英文并存）。 */
export function outboxStatusLabel(status: string): string {
  switch (status) {
    case "pending":
      return "待发送";
    case "sending":
      return "发送中";
    case "sent":
      return "已发送";
    case "dead":
      return "投递失败";
    default:
      return status;
  }
}

/** 通知渠道类型 → 展示名。 */
export function channelLabel(channelType?: string): string {
  switch (channelType) {
    case "telegram":
      return "Telegram";
    case "http_webhook":
      return "HTTP Webhook";
    default:
      return channelType || "";
  }
}

/** 事件类型 → 展示短名（仪表盘事件行用）。Issue/PR/Actions/Star/Watch 与侧栏品牌词一致；
 * 三类安全告警复用 alertKindLabel 中文名，避免同一 kind 在仪表盘与安全页文案分叉。 */
export function eventKindLabel(kind: string): string {
  switch (kind) {
    case "issue":
      return "Issue";
    case "pull_request":
      return "PR";
    case "workflow_run":
      return "Actions";
    case "star":
      return "Star";
    case "watch":
      return "Watch";
    default:
      return alertKindLabel(kind);
  }
}

/** 事件动作 → 中文文案（仪表盘事件行用）；star/watch 动作依赖 kind 区分，避免与 issue/alert 的 created 撞文案。 */
export function eventActionLabel(action: string, kind?: string): string {
  if (kind === "star") {
    if (action === "created") return "已收藏";
    if (action === "deleted") return "取消收藏";
  }
  if (kind === "watch") {
    if (action === "started") return "已关注";
  }
  switch (action) {
    case "opened":
      return "已打开";
    case "closed":
      return "已关闭";
    case "reopened":
      return "重新打开";
    case "merged":
      return "已合并";
    case "completed":
      return "已完成";
    case "recovered":
      return "已恢复";
    case "updated":
      return "已更新";
    case "created":
      return "新告警";
    case "dismissed":
      return "已忽略";
    case "fixed":
    case "resolved":
      return "已修复";
    case "ready_for_review":
      return "待审核";
    case "converted_to_draft":
      return "转为草稿";
    case "auto_dismissed":
      return "自动忽略";
    default:
      return action;
  }
}

/** Issue/PR 状态 → 中文文案（列表页徽章用，与筛选按钮文案一致）。 */
export function workItemStateLabel(state: string): string {
  switch (state) {
    case "open":
      return "未关闭";
    case "closed":
      return "已关闭";
    default:
      return state || "—";
  }
}

/** Actions 结论/状态 → 中文文案（Actions 列表页徽章用，与仪表盘状态语义一致）。 */
export function workflowConclusionLabel(conclusion: string): string {
  switch (conclusion) {
    case "success":
      return "成功";
    case "failure":
    case "startup_failure":
      return "失败";
    case "cancelled":
      return "已取消";
    case "timed_out":
      return "超时";
    case "action_required":
      return "需处理";
    case "skipped":
      return "已跳过";
    case "in_progress":
    case "queued":
    case "pending":
      return "进行中";
    default:
      return conclusion || "—";
  }
}

/** 安全告警严重度 → 中文文案（与后端通知文案严重度中文保持一致）。 */
export function severityLabel(severity: string): string {
  switch (severity) {
    case "critical":
      return "严重";
    case "high":
    case "error":
      return "高";
    case "medium":
    case "warning":
      return "中";
    case "low":
    case "note":
      return "低";
    default:
      return severity || "";
  }
}

/** 安全告警状态 → 中文文案（安全列表页用）。 */
export function alertStateLabel(state: string): string {
  switch (state) {
    case "open":
      return "待处理";
    case "dismissed":
      return "GitHub 已忽略";
    case "fixed":
    case "resolved":
      return "已修复";
    case "auto_dismissed":
      return "自动忽略";
    default:
      return state || "—";
  }
}

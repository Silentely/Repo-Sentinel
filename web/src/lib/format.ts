/**
 * 将 ISO 日期字符串格式化为相对时间（中文）。
 * 用于列表与仪表盘共用。
 */
export function formatRelativeTime(dateString: string): string {
  if (!dateString) return "";
  const date = new Date(dateString);
  if (isNaN(date.getTime())) return "";
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  if (diffMs < 0) return "";
  const diffSeconds = Math.floor(diffMs / 1000);
  const diffMinutes = Math.floor(diffSeconds / 60);
  const diffHours = Math.floor(diffMinutes / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffSeconds < 60) return "刚刚";
  if (diffMinutes < 60) return `${diffMinutes} 分钟前`;
  if (diffHours < 24) return `${diffHours} 小时前`;
  if (diffDays < 30) return `${diffDays} 天前`;
  return date.toLocaleDateString("zh-CN");
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

/** 事件类型 → 英文短名（仪表盘事件行用）。 */
export function eventKindLabel(kind: string): string {
  switch (kind) {
    case "issue":
      return "Issue";
    case "pull_request":
      return "PR";
    case "workflow_run":
      return "Actions";
    case "dependabot":
      return "Dependabot";
    case "code_scanning":
      return "Code Scan";
    case "secret_scanning":
      return "Secret";
    default:
      return kind;
  }
}

/** 事件动作 → 中文文案（仪表盘事件行用）。 */
export function eventActionLabel(action: string): string {
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

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

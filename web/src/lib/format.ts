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

/**
 * Outbox 投递错误码 → 中文排障提示（投递记录页用）。
 * 机器码保留展示（便于对照日志），说明文字帮助普通用户判断下一步动作；
 * 未收录的错误码返回空串，不占用展示空间。
 */
export function outboxErrorHint(errorCode: string): string {
  switch (errorCode) {
    case "telegram_not_configured":
      return "Telegram 渠道缺少 Bot Token 或 Chat ID，请到「渠道配置」补全。";
    case "telegram_rate_limited":
      return "Telegram 触发限流，已按上游建议推迟重试，通常会自动恢复。";
    case "telegram_http_500":
    case "telegram_http_502":
    case "telegram_http_503":
      return "Telegram 服务端暂时不可用，系统会按退避策略自动重试。";
    case "telegram_client_error_400":
      return "Telegram 拒绝消息：多为 Chat ID 无效或正文格式问题，请核对渠道目标。";
    case "telegram_client_error_401":
      return "Telegram Bot Token 无效或已失效，请到「渠道配置」更新 Token。";
    case "telegram_client_error_403":
      return "机器人无法访问该会话：用户需先向机器人发起对话，或机器人已被屏蔽。";
    case "telegram_client_error_404":
      return "Telegram Chat ID 不存在，请核对渠道目标是否填写正确。";
    case "http_webhook_retry_after":
      return "接收端要求按 Retry-After 推迟重试，系统已遵循该指引。";
    case "http_webhook_status_408":
    case "http_webhook_status_425":
    case "http_webhook_status_429":
      return "接收端限流或繁忙，系统将按退避策略自动重试。";
    case "http_webhook_status_500":
    case "http_webhook_status_502":
    case "http_webhook_status_503":
    case "http_webhook_status_504":
      return "接收端服务端错误，请检查 Webhook 目标服务是否正常。";
    case "decrypt_secret":
      return "渠道密钥解密失败：可能主密钥已更换或数据损坏，请重新保存渠道配置。";
    case "missing_keyring":
      return "未配置加密主密钥，无法解密渠道密钥，请检查部署配置。";
    case "unknown_channel":
      return "未知渠道类型，该记录无法投递，请检查渠道配置。";
    case "database_unavailable":
      return "数据库暂时不可用，投递队列等待恢复后继续。";
    case "outbox_mark_failed":
      return "投递状态回写失败，条目将在锁超时后重新尝试。";
    case "normalize_failed":
      return "Webhook 载荷无法解析，事件被丢弃，请核对 GitHub 事件格式。";
    case "rule_failed":
      return "通知规则评估失败，事件未进入投递，请查看服务端日志。";
    default:
      return "";
  }
}

import { queryOptions } from "@tanstack/react-query";

import { apiRequest } from "../../lib/api/client";

export interface DashboardStats {
  open_issues: number;
  open_pulls: number;
  failed_actions: number;
  open_security: number;
  events_24h: number;
  outbox_dead: number;
  repos_active: number;
  repos_baseline: number;
  channels_enabled: number;
}

export interface Repository {
  id: string;
  type: string;
  sync_status: string;
  full_name: string;
  owner: string;
  name: string;
  is_private: boolean;
  is_archived: boolean;
  monitor_enabled: boolean;
  issues_enabled: boolean;
  pr_enabled: boolean;
  actions_enabled: boolean;
  alerts_enabled: boolean;
  stars_enabled: boolean;
  watches_enabled: boolean;
  html_url: string;
  updated_at: string;
  default_branch?: string;
  last_synced_at?: string;
  last_sync_error_code?: string;
}

export interface MonitorEvent {
  id: string;
  kind: string;
  action: string;
  title: string;
  severity: string;
  actor: string;
  html_url: string;
  occurred_at: string;
  repository_id?: string;
}

export interface OutboxItem {
  id: string;
  channel_id: string;
  channel_type: string;
  status: string;
  title: string;
  attempt_count: number;
  last_error_code: string;
  html_url: string;
  created_at: string;
  updated_at: string;
  /** Telegram HTML 格式正文（详情抽屉纯文本化展示）。 */
  body_text?: string;
}

/** 通知渠道类型字面量（后端仅两种，收窄后无需再断言）。 */
export type ChannelType = "telegram" | "http_webhook";

export interface NotificationChannelRow {
  id: string;
  channel_type: ChannelType;
  name: string;
  enabled: boolean;
  target: string;
  secret_configured: boolean;
  // 订阅的实时通知类型；null 表示全部订阅。
  event_kinds: string[] | null;
  digest_enabled: boolean;
  updated_at?: string;
}

export const channelsQueryOptions = queryOptions({
  queryKey: ["channels"] as const,
  queryFn: () => apiRequest<{ items: NotificationChannelRow[] }>("/api/v1/notifications/channels"),
  staleTime: 15_000,
});

export interface Page<T> {
  items: T[];
  page: number;
  per_page: number;
  total: number;
}

export const dashboardQueryOptions = queryOptions({
  queryKey: ["dashboard"] as const,
  queryFn: () => apiRequest<DashboardStats>("/api/v1/dashboard"),
  staleTime: 15_000,
  refetchInterval: 30_000,
});

export const repositoriesQueryOptions = queryOptions({
  queryKey: ["repositories"] as const,
  // 列表筛选下拉与仪表盘共用；上限对齐后端 normalizePage(100)。
  queryFn: () => apiRequest<Page<Repository>>("/api/v1/repositories?per_page=100"),
  staleTime: 15_000,
});

/** 仪表盘「通知投递」与「最近事件」面板的展示上限：只取最近 N 条，避免长列表拖垮首屏。 */
export const DASHBOARD_FEED_LIMIT = 15;

/** 事件列表查询（perPage 可调）：仪表盘取最近 15 条，列表页可放大。 */
export const eventsQueryOptions = (perPage = 30) =>
  queryOptions({
    queryKey: ["events", perPage] as const,
    queryFn: () => apiRequest<Page<MonitorEvent>>(`/api/v1/events?per_page=${perPage}`),
    staleTime: 15_000,
    refetchInterval: 30_000,
  });

/** Outbox 分页查询（参数化：status / channel_type / per_page），仪表盘与发件箱页共用单一实现。 */
export const outboxQueryOptions = (status = "", channelType = "", perPage = 50) =>
  queryOptions({
    queryKey: ["outbox", status, channelType, perPage] as const,
    queryFn: () => {
      const params = new URLSearchParams({ per_page: String(perPage) });
      if (status) params.set("status", status);
      if (channelType) params.set("channel_type", channelType);
      return apiRequest<Page<OutboxItem>>(`/api/v1/notifications/outbox?${params.toString()}`);
    },
    staleTime: 15_000,
    refetchInterval: 15_000,
  });

export async function activateRepository(id: string): Promise<Repository> {
  return apiRequest<Repository>(`/api/v1/repositories/${id}/activate`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

/** 彻底删除仓库并级联清理全部本地数据（PR/Issue、事件、告警、快照、游标、待投递通知），不可恢复。 */
export async function deleteRepository(id: string): Promise<void> {
  await apiRequest(`/api/v1/repositories/${id}`, { method: "DELETE" });
}

export async function retryOutbox(id: string): Promise<void> {
  await apiRequest(`/api/v1/notifications/outbox/${id}/retry`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function upsertChannel(
  type: "telegram" | "http_webhook",
  body: {
    name?: string;
    enabled: boolean;
    target: string;
    secret?: string;
    // 订阅的实时通知类型；不传则保留后端现值。
    event_kinds?: string[];
    digest_enabled?: boolean;
  },
): Promise<void> {
  await apiRequest(`/api/v1/notifications/channels/${type}`, {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function testChannel(type: "telegram" | "http_webhook"): Promise<{ status: string }> {
  return apiRequest(`/api/v1/notifications/channels/${type}/test`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function deleteChannel(type: "telegram" | "http_webhook"): Promise<void> {
  await apiRequest(`/api/v1/notifications/channels/${type}`, {
    method: "DELETE",
  });
}

export async function toggleChannel(type: "telegram" | "http_webhook", enabled: boolean): Promise<void> {
  await apiRequest(`/api/v1/notifications/channels/${type}/toggle`, {
    method: "PATCH",
    body: JSON.stringify({ enabled }),
  });
}

export async function reconcileRepository(id: string): Promise<void> {
  await apiRequest(`/api/v1/repositories/${id}/reconcile`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export interface RepositorySettings {
  monitor_enabled?: boolean;
  issues_enabled?: boolean;
  pr_enabled?: boolean;
  actions_enabled?: boolean;
  alerts_enabled?: boolean;
  stars_enabled?: boolean;
  watches_enabled?: boolean;
  is_archived?: boolean;
}

export async function updateRepositorySettings(id: string, settings: RepositorySettings): Promise<Repository> {
  return apiRequest<Repository>(`/api/v1/repositories/${id}/settings`, {
    method: "PATCH",
    body: JSON.stringify(settings),
  });
}

/** 设置 Issue/PR 本地忽略标记（不回写 GitHub）。 */
export async function setWorkItemIgnored(id: string, ignored: boolean): Promise<void> {
  await apiRequest(`/api/v1/work-items/${id}/ignored`, {
    method: "PATCH",
    body: JSON.stringify({ ignored }),
  });
}

/** 设置 Workflow Run 本地忽略标记。 */
export async function setWorkflowRunIgnored(id: string, ignored: boolean): Promise<void> {
  await apiRequest(`/api/v1/workflow-runs/${id}/ignored`, {
    method: "PATCH",
    body: JSON.stringify({ ignored }),
  });
}

/** 设置安全告警本地忽略标记（与 GitHub dismissed 独立）。 */
export async function setSecurityAlertIgnored(id: string, ignored: boolean): Promise<void> {
  await apiRequest(`/api/v1/security-alerts/${id}/ignored`, {
    method: "PATCH",
    body: JSON.stringify({ ignored }),
  });
}

export async function reconcileAll(): Promise<void> {
  await apiRequest("/api/v1/sync/reconcile", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export interface GitHubConfigStatus {
  app_id_configured: boolean;
  client_id_configured: boolean;
  private_key_configured: boolean;
  webhook_secret_configured: boolean;
  webhook_previous_secret_configured?: boolean;
  external_pat_configured: boolean;
  webhook_path: string;
  app_id_source?: string;
  client_id_source?: string;
  private_key_source?: string;
  webhook_secret_source?: string;
  public_base_url_source?: string;
}

export interface GitHubRuntimeConfig {
  app_id: number;
  client_id: string;
  private_key_path: string;
  public_base_url: string;
  webhook_path: string;
  webhook_url: string;
  app_id_configured: boolean;
  client_id_configured: boolean;
  private_key_configured: boolean;
  webhook_secret_configured: boolean;
  external_pat_configured: boolean;
  app_id_source: string;
  client_id_source: string;
  private_key_source: string;
  webhook_secret_source: string;
  public_base_url_source: string;
  app_id_locked: boolean;
  client_id_locked: boolean;
  private_key_locked: boolean;
  webhook_secret_locked: boolean;
  public_base_url_locked: boolean;
  can_edit_in_ui: boolean;
  note: string;
}

export interface GitHubRuntimeConfigInput {
  app_id?: number;
  client_id?: string;
  private_key_path?: string;
  private_key_pem?: string;
  webhook_secret?: string;
  public_base_url?: string;
  clear_private_key?: boolean;
  clear_webhook_secret?: boolean;
}

export interface VersionInfo {
  version?: string;
  git_sha?: string;
  git_branch?: string;
  build_time?: string;
  build_channel?: string;
  go_version?: string;
  database_driver?: string;
  schema_version?: string;
  update_check_enabled?: boolean;
  public_base_url?: string;
  http_addr?: string;
  github?: GitHubConfigStatus;
}

export interface UpdateCheckInfo {
  enabled: boolean;
  latest_version?: string;
  latest_url?: string;
  update_available: boolean;
  checked_at?: string;
  error?: string;
  source?: string;
  cached?: boolean;
}

export interface VersionCheckResponse {
  version: VersionInfo;
  update_check: UpdateCheckInfo;
}

export interface SystemSettings {
  "admin.timezone"?: string;
  "digest.local_time"?: string;
  "digest.send_empty"?: boolean;
  "notify.aggregate_window_sec"?: number;
  "notify.burst_threshold"?: number;
  "display.closed_limit"?: number;
  "notify.burst_window_sec"?: number;
  "retention.events_days"?: number;
  "retention.outbox_days"?: number;
  "retention.webhook_deliveries_days"?: number;
  "feature.issues"?: boolean;
  "feature.pull_requests"?: boolean;
  "feature.actions"?: boolean;
  "feature.security_alerts"?: boolean;
  "feature.stars"?: boolean;
  "feature.watches"?: boolean;
  "feature.starred_releases"?: boolean;
  "report.weekly_enabled"?: boolean;
  "report.weekly_day"?: string;
  "report.monthly_enabled"?: boolean;
  "report.monthly_day"?: number;
}

export interface Installation {
  id: string;
  installation_id: number;
  account_login: string;
  account_type: string;
  suspended: string;
}

export const versionQueryOptions = queryOptions({
  queryKey: ["version"] as const,
  queryFn: () => apiRequest<VersionInfo>("/api/v1/system/version"),
  staleTime: 30_000,
});

export const settingsQueryOptions = queryOptions({
  queryKey: ["system-settings"] as const,
  queryFn: () => apiRequest<SystemSettings>("/api/v1/system/settings"),
  staleTime: 30_000,
});

export const installationsQueryOptions = queryOptions({
  queryKey: ["installations"] as const,
  queryFn: () => apiRequest<{ items: Installation[] }>("/api/v1/github/installations"),
  staleTime: 15_000,
});

export async function checkForUpdates(force = true): Promise<VersionCheckResponse> {
  return apiRequest<VersionCheckResponse>(`/api/v1/system/version/check?force=${force ? "true" : "false"}`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function saveSystemSettings(body: SystemSettings): Promise<SystemSettings> {
  return apiRequest<SystemSettings>("/api/v1/system/settings", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function addExternalRepository(fullName: string): Promise<Repository> {
  return apiRequest<Repository>("/api/v1/repositories/external", {
    method: "POST",
    body: JSON.stringify({ full_name: fullName }),
  });
}

export const githubConfigQueryOptions = queryOptions({
  queryKey: ["github-config"] as const,
  queryFn: () => apiRequest<GitHubRuntimeConfig>("/api/v1/github/config"),
  staleTime: 10_000,
});

export async function saveGitHubConfig(body: GitHubRuntimeConfigInput): Promise<GitHubRuntimeConfig> {
  return apiRequest<GitHubRuntimeConfig>("/api/v1/github/config", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

// ---- AI 集成配置（管理台可编辑；API Key 加密存储，不回显明文）----

export interface AIConfig {
  enabled: boolean;
  base_url: string;
  model: string;
  timeout_sec: number;
  max_tokens: number;
  retries: number;
  digest_enabled: boolean;
  triage_enabled: boolean;
  release_summary_enabled: boolean;
  api_key_configured: boolean;
  enabled_source: string;
  base_url_source: string;
  model_source: string;
  timeout_source: string;
  max_tokens_source: string;
  retries_source: string;
  api_key_source: string;
  digest_enabled_source: string;
  triage_enabled_source: string;
  release_summary_enabled_source: string;
  enabled_locked: boolean;
  base_url_locked: boolean;
  model_locked: boolean;
  timeout_locked: boolean;
  max_tokens_locked: boolean;
  retries_locked: boolean;
  api_key_locked: boolean;
  digest_enabled_locked: boolean;
  triage_enabled_locked: boolean;
  release_summary_enabled_locked: boolean;
  can_edit_in_ui: boolean;
  note: string;
}

export interface AIConfigInput {
  enabled?: boolean;
  base_url?: string;
  model?: string;
  timeout_sec?: number;
  max_tokens?: number;
  retries?: number;
  digest_enabled?: boolean;
  triage_enabled?: boolean;
  release_summary_enabled?: boolean;
  api_key?: string;
  clear_api_key?: boolean;
}

export const aiConfigQueryOptions = queryOptions({
  queryKey: ["ai-config"] as const,
  queryFn: () => apiRequest<AIConfig>("/api/v1/ai/config"),
  staleTime: 10_000,
});

export async function saveAIConfig(body: AIConfigInput): Promise<AIConfig> {
  return apiRequest<AIConfig>("/api/v1/ai/config", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

// ---- Star Release 追踪 ----

export interface StarredReleasesConfig {
  username: string;
  star_sync_interval: string;
  release_poll_interval: string;
  max_trackers: number;
  notify_prerelease: boolean;
  enabled: boolean;
  ai_release_summary_enabled: boolean;
  counts: {
    tracking: number;
    inactive: number;
    disabled: number;
    unavailable: number;
  };
}

export interface StarredReleasesConfigInput {
  username?: string;
  star_sync_interval?: string;
  release_poll_interval?: string;
  max_trackers?: number;
  notify_prerelease?: boolean;
  enabled?: boolean;
}

export interface StarredTrackerItem {
  id: string;
  full_name: string;
  state: "tracking" | "inactive" | "disabled" | "unavailable";
  last_release_tag?: string;
  last_release_published_at?: string;
  last_poll_at?: string;
  first_seen_at: string;
}

export interface StarredTrackerList {
  items: StarredTrackerItem[];
  page: number;
  per_page: number;
  total: number;
}

export const starredReleasesConfigQueryOptions = queryOptions({
  queryKey: ["starred-releases-config"] as const,
  queryFn: () => apiRequest<StarredReleasesConfig>("/api/v1/starred-releases/config"),
  staleTime: 10_000,
});

export async function saveStarredReleasesConfig(body: StarredReleasesConfigInput): Promise<StarredReleasesConfig> {
  return apiRequest<StarredReleasesConfig>("/api/v1/starred-releases/config", {
    method: "PUT",
    body: JSON.stringify(body),
  });
}

export async function syncStarredReleases(): Promise<{ started: boolean }> {
  return apiRequest<{ started: boolean }>("/api/v1/starred-releases/sync", {
    method: "POST",
  });
}

export async function listStarredTrackers(params: {
  page?: number;
  per_page?: number;
  state?: string;
} = {}): Promise<StarredTrackerList> {
  const query = new URLSearchParams();
  if (params.page != null) query.set("page", String(params.page));
  if (params.per_page != null) query.set("per_page", String(params.per_page));
  if (params.state) query.set("state", params.state);
  const qs = query.toString();
  return apiRequest<StarredTrackerList>(`/api/v1/starred-releases/trackers${qs ? `?${qs}` : ""}`);
}

export async function setStarredTrackerState(id: string, state: "disabled" | "tracking"): Promise<{ ok: boolean }> {
  return apiRequest<{ ok: boolean }>(`/api/v1/starred-releases/trackers/${id}/state`, {
    method: "POST",
    body: JSON.stringify({ state }),
  });
}

export interface AIConnectivityTestResult {
  ok: boolean;
  message: string;
  model: string;
  base_url: string;
  latency_ms: number;
}

/** 连通性测试：以当前生效配置（未锁定字段可临时覆盖）发送一次最小对话，返回可达性与耗时。
 *  连通性测试允许等待较长时间（默认 60s），避免应用级 20s 请求超时把慢端点的正常响应误判为失败。 */
export async function testAIConnectivity(body: AIConfigInput, timeoutMs = 60_000): Promise<AIConnectivityTestResult> {
  return apiRequest<AIConnectivityTestResult>("/api/v1/ai/test", {
    method: "POST",
    body: JSON.stringify(body),
    signal: AbortSignal.timeout(timeoutMs),
  });
}

export interface SyncInstallationReposResult {
  installations: number;
  imported_or_updated: number;
  last_error?: string;
}

/** 用 Installation Token 从 GitHub 拉取已授权仓库并写入本地（基线）。 */
export async function syncInstallationRepositories(): Promise<SyncInstallationReposResult> {
  return apiRequest<SyncInstallationReposResult>("/api/v1/github/sync-repositories", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

/** Star 增长趋势序列点（后端 /api/v1/stats/star-trend 返回）。 */
export interface StarTrendPoint {
  date: string;
  total: number;
}

/** Star 增长趋势查询：days 为 7/30/90/0（0 表示全部），跟随查询缓存切换数据。 */
export function starTrendQueryOptions(days: number) {
  return queryOptions({
    queryKey: ["star-trend", days],
    queryFn: async (): Promise<StarTrendPoint[]> => {
      const data = await apiRequest<{ items: StarTrendPoint[] }>(`/api/v1/stats/star-trend?days=${days}`);
      return data.items ?? [];
    },
  });
}

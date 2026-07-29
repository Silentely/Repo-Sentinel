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
  html_url: string;
  updated_at: string;
  // 新增字段：展示后端已有但前端未展示的数据
  default_branch?: string;
  last_synced_at?: string;
  last_sync_error_code?: string;
  baseline_started_at?: string;
  baseline_finished_at?: string;
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
  suppress_notification: boolean;
  // 新增字段
  repository_id?: string;
  subject_number?: number;
  workflow_conclusion?: string;
}

export interface OutboxItem {
  id: string;
  status: string;
  title: string;
  attempt_count: number;
  last_error_code: string;
  created_at: string;
}

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
  queryFn: () => apiRequest<Page<Repository>>("/api/v1/repositories?per_page=50"),
  staleTime: 15_000,
});

export const eventsQueryOptions = queryOptions({
  queryKey: ["events"] as const,
  queryFn: () => apiRequest<Page<MonitorEvent>>("/api/v1/events?per_page=30"),
  staleTime: 15_000,
  refetchInterval: 30_000,
});

export const outboxQueryOptions = queryOptions({
  queryKey: ["outbox"] as const,
  queryFn: () => apiRequest<Page<OutboxItem>>("/api/v1/notifications/outbox?per_page=30"),
  staleTime: 15_000,
});

export async function activateRepository(id: string): Promise<Repository> {
  return apiRequest<Repository>(`/api/v1/repositories/${id}/activate`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function retryOutbox(id: string): Promise<void> {
  await apiRequest(`/api/v1/notifications/outbox/${id}/retry`, {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function upsertChannel(
  type: "telegram" | "http_webhook",
  body: { name?: string; enabled: boolean; target: string; secret?: string },
): Promise<void> {
  await apiRequest(`/api/v1/notifications/channels/${type}`, {
    method: "PUT",
    body: JSON.stringify(body),
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
  is_archived?: boolean;
}

export async function updateRepositorySettings(id: string, settings: RepositorySettings): Promise<Repository> {
  return apiRequest<Repository>(`/api/v1/repositories/${id}/settings`, {
    method: "PATCH",
    body: JSON.stringify(settings),
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

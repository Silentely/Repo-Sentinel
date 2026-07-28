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
  html_url: string;
  updated_at: string;
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

export async function reconcileAll(): Promise<void> {
  await apiRequest("/api/v1/sync/reconcile", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

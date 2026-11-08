import { isTelegramWebAppAvailable } from "./tma-sdk";
export interface TmaWorkItem {
  id: string;
  repository_id: string;
  number: number;
  kind: string;
  title: string;
  state: string;
  html_url: string;
  updated_at: string;
  ignored?: boolean;
  [key: string]: unknown;
}

export interface TmaDashboardData {
  monitored_repos_count: number;
  pending_work_items_count: number;
  webhook_deliveries_total: number;
  webhook_deliveries_success: number;
  webhook_delivery_rate: number;
  quiet_hours_enabled: boolean;
  quiet_hours_start: string;
  quiet_hours_end: string;
  quiet_hours_tz: string;
  work_items: TmaWorkItem[];
}

export interface TmaEvent {
  id: string;
  kind: string;
  action: string;
  title: string;
  severity: string;
  actor: string;
  html_url: string;
  occurred_at: string;
  repository_id?: string;
  subject_number?: number;
  workflow_run_id?: number;
  payload_summary?: Record<string, unknown>;
  [key: string]: unknown;
}

export interface TmaRepository {
  id: string;
  full_name: string;
  owner: string;
  name: string;
  is_private: boolean;
  is_archived: boolean;
  monitor_enabled: boolean;
  html_url: string;
  [key: string]: unknown;
}

export interface TmaWorkflowRun {
  id: string;
  repository_id: string;
  run_id: number;
  workflow_name: string;
  head_branch: string;
  head_sha: string;
  conclusion?: string;
  status: string;
  html_url: string;
  run_attempt: number;
  [key: string]: unknown;
}

export interface TmaEventDetailData {
  event: TmaEvent;
  repository: TmaRepository;
  work_item?: TmaWorkItem | null;
  workflow_run?: TmaWorkflowRun | null;
}

export interface TmaActionOutcome {
  status: "already_done" | "executed" | "ok" | string;
  message?: string;
  action?: string;
  external_id?: string;
}

export interface TmaActionPayload {
  event_id: string;
  action: string;
  comment_body?: string;
}

export interface TmaApiErrorInit {
  status: number;
  errorCode: string;
  message: string;
  details?: Record<string, unknown>;
  isPendingConflict?: boolean;
}

export class TmaApiError extends Error {
  readonly status: number;
  readonly errorCode: string;
  readonly details?: Record<string, unknown>;
  readonly isPendingConflict: boolean;

  constructor(init: TmaApiErrorInit) {
    super(init.message || "请求失败，请稍后重试。");
    this.name = "TmaApiError";
    this.status = init.status;
    this.errorCode = init.errorCode;
    this.details = init.details;
    this.isPendingConflict = init.isPendingConflict ?? (init.status === 409);
  }
}

export class TmaUnavailableError extends Error {
  constructor(message = "Telegram WebApp 环境不可用，已拦截外部网络调用。") {
    super(message);
    this.name = "TmaUnavailableError";
  }
}

export class TmaConflictError extends TmaApiError {
  constructor(init: Omit<TmaApiErrorInit, "status" | "isPendingConflict">) {
    super({
      ...init,
      status: 409,
      isPendingConflict: true,
      errorCode: init.errorCode || "conflict",
    });
    this.name = "TmaConflictError";
  }
}

export function isTmaConflictError(error: unknown): boolean {
  if (error instanceof TmaApiError) {
    return error.isPendingConflict || error.status === 409;
  }
  if (typeof error === "object" && error !== null) {
    const rec = error as Record<string, unknown>;
    return rec.isPendingConflict === true || rec.status === 409 || rec.errorCode === "conflict";
  }
  return false;
}

const TMA_BASE = "/api/v1/tma";

export function getTmaInitData(): string {
  if (typeof window === "undefined") {
    return "";
  }
  return window.Telegram?.WebApp?.initData ?? "";
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

async function parseTmaApiError(response: Response): Promise<TmaApiError> {
  const raw = await response.text().catch(() => "");
  let payload: unknown;
  try {
    payload = JSON.parse(raw);
  } catch {
    payload = undefined;
  }

  const record = isRecord(payload) ? payload : {};
  const errorCode = typeof record.error_code === "string" ? record.error_code : "http_error";
  let message =
    typeof record.message === "string" && record.message.trim() !== ""
      ? record.message
      : "";

  if (message === "") {
    const text = raw.trim();
    if (text !== "" && !text.startsWith("{")) {
      message = text.length > 300 ? `${text.slice(0, 300)}…` : text;
    } else if (response.status === 409) {
      message = "操作正在执行中，请勿重复操作。";
    } else {
      message = "服务器暂时无法完成请求，请稍后重试。";
    }
  }

  const details = isRecord(record.details) ? record.details : undefined;

  if (response.status === 409) {
    return new TmaConflictError({
      errorCode,
      message,
      details,
    });
  }

  return new TmaApiError({
    status: response.status,
    errorCode,
    message,
    details,
    isPendingConflict: false,
  });
}

export async function tmaRequest<T>(
  endpoint: string,
  options: RequestInit = {},
): Promise<T> {
  if (!isTelegramWebAppAvailable()) {
    throw new TmaUnavailableError();
  }

  const url = endpoint.startsWith("/") ? endpoint : `${TMA_BASE}/${endpoint}`;
  const headers = new Headers(options.headers);
  headers.set("Accept", "application/json");
  if (options.body !== undefined && !headers.has("Content-Type")) {
    headers.set("Content-Type", "application/json");
  }

  const initData = getTmaInitData();
  headers.set("X-Telegram-Init-Data", initData);

  const response = await fetch(url, {
    ...options,
    headers,
  });

  if (!response.ok) {
    throw await parseTmaApiError(response);
  }

  if (response.status === 204) {
    return undefined as T;
  }

  const raw = await response.text().catch(() => "");
  try {
    return JSON.parse(raw) as T;
  } catch {
    throw new TmaApiError({
      status: response.status,
      errorCode: "invalid_response",
      message: raw
        ? `服务端返回了无法解析的响应（${raw.slice(0, 120)}${raw.length > 120 ? "…" : ""}）。`
        : "服务端返回了空响应。",
    });
  }
}

export async function fetchTmaDashboard(signal?: AbortSignal): Promise<TmaDashboardData> {
  return tmaRequest<TmaDashboardData>(`${TMA_BASE}/dashboard`, {
    method: "GET",
    signal,
  });
}

export async function fetchTmaEvent(id: string, signal?: AbortSignal): Promise<TmaEventDetailData> {
  return tmaRequest<TmaEventDetailData>(`${TMA_BASE}/events/${encodeURIComponent(id)}`, {
    method: "GET",
    signal,
  });
}

export async function fetchTmaEventDiff(id: string, signal?: AbortSignal): Promise<{ diff: string }> {
  return tmaRequest<{ diff: string }>(`${TMA_BASE}/events/${encodeURIComponent(id)}/diff`, {
    method: "GET",
    signal,
  });
}

export async function fetchTmaEventLogs(
  id: string,
  signal?: AbortSignal,
): Promise<{ logs: string; truncated: boolean; job_id?: number; job_name?: string }> {
  return tmaRequest<{ logs: string; truncated: boolean; job_id?: number; job_name?: string }>(
    `${TMA_BASE}/events/${encodeURIComponent(id)}/logs`,
    {
      method: "GET",
      signal,
    },
  );
}

export async function executeTmaAction(
  payload: TmaActionPayload,
  signal?: AbortSignal,
): Promise<TmaActionOutcome> {
  return tmaRequest<TmaActionOutcome>(`${TMA_BASE}/actions`, {
    method: "POST",
    body: JSON.stringify(payload),
    signal,
  });
}

export async function toggleTmaQuietHours(
  enabled: boolean,
  signal?: AbortSignal,
): Promise<{ enabled: boolean }> {
  return tmaRequest<{ enabled: boolean }>(`${TMA_BASE}/quiet-hours`, {
    method: "POST",
    body: JSON.stringify({ enabled }),
    signal,
  });
}

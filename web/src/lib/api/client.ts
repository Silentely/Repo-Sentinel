import type { QueryClient } from "@tanstack/react-query";

import { queryClient as applicationQueryClient } from "../query-client";
import { ApiError } from "./errors";

const csrfCookieName = "reposentinel_csrf";
const csrfHeaderName = "X-CSRF-Token";
// 请求默认 20s 超时：弱网/服务挂起时让查询与变更可以失败重试，而不是永久 pending。
const defaultTimeoutMs = 20_000;

export type FetchLike = (input: RequestInfo | URL, init?: RequestInit) => Promise<Response>;

export interface ApiClientDependencies {
  fetchImpl?: FetchLike;
  queryClient?: Pick<QueryClient, "clear">;
  onUnauthorized?: () => void;
  readCookie?: (name: string) => string | undefined;
}

export type ApiRequest = <T>(path: string, init?: RequestInit) => Promise<T>;

export function createApiClient(dependencies: ApiClientDependencies = {}): ApiRequest {
  const fetchImpl = dependencies.fetchImpl ?? ((input, init) => fetch(input, init));
  const readCookie = dependencies.readCookie ?? readBrowserCookie;

  return async <T>(path: string, init: RequestInit = {}): Promise<T> => {
    const headers = new Headers(init.headers);
    headers.set("Accept", "application/json");
    if (init.body !== undefined && !headers.has("Content-Type")) {
      headers.set("Content-Type", "application/json");
    }

    const method = (init.method ?? "GET").toUpperCase();
    if (!isSafeMethod(method)) {
      const csrfToken = readCookie(csrfCookieName);
      if (csrfToken) {
        headers.set(csrfHeaderName, csrfToken);
      }
    }

    const response = await fetchImpl(path, {
      ...init,
      method,
      headers,
      credentials: init.credentials ?? "same-origin",
      signal: init.signal ?? AbortSignal.timeout(defaultTimeoutMs),
    });

    if (!response.ok) {
      if (response.status === 401) {
        dependencies.queryClient?.clear();
        dependencies.onUnauthorized?.();
      }
      throw await parseApiError(response);
    }

    if (response.status === 204) {
      return undefined as T;
    }
    // 200 成功路径也需解析兜底：反代透传 HTML 错误页或空体时 response.json() 会抛
    // SyntaxError，若任其冒泡会被 toApiError 误判为 network_error「无法连接」。
    // 此处构造 invalid_response 并附原文摘要，便于判断是网关还是上游问题。
    try {
      return (await response.json()) as T;
    } catch {
      const raw = await response.text().catch(() => "");
      throw new ApiError({
        status: response.status,
        errorCode: "invalid_response",
        message: raw ? `服务端返回了无法解析的响应（${raw.slice(0, 120)}${raw.length > 120 ? "…" : ""}）。` : "服务端返回了空响应。",
        details: undefined,
      });
    }
  };
}

export const apiRequest = createApiClient({
  queryClient: applicationQueryClient,
  onUnauthorized: redirectToLogin,
});

function isSafeMethod(method: string): boolean {
  return method === "GET" || method === "HEAD" || method === "OPTIONS";
}

function readBrowserCookie(name: string): string | undefined {
  if (typeof document === "undefined" || !document.cookie) {
    return undefined;
  }
  for (const item of document.cookie.split(";")) {
    const [rawName, ...rawValue] = item.trim().split("=");
    if (rawName === name) {
      try {
        return decodeURIComponent(rawValue.join("="));
      } catch {
        return undefined;
      }
    }
  }
  return undefined;
}

async function parseApiError(response: Response): Promise<ApiError> {
  const raw = await response.text();
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
    // 非标准 JSON 错误（反代 / 网关返回的纯文本或 HTML 错误页）：回退展示截断原文，
    // 避免通用文案吞掉真实原因（如 Envoy 的 upstream connect error）。
    if (text !== "" && !text.startsWith("{")) {
      message = text.length > 300 ? `${text.slice(0, 300)}…` : text;
    } else {
      message = "服务器暂时无法完成请求，请稍后重试。";
    }
  }

  const details = isRecord(record.details) ? record.details : undefined;

  return new ApiError({ status: response.status, errorCode, message, details });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function redirectToLogin(): void {
  if (typeof window === "undefined" || !window.location || window.location.pathname === "/login") {
    return;
  }
  if (typeof window.history !== "undefined" && typeof window.history.replaceState === "function") {
    window.history.replaceState(null, "", "/login");
  }
  if (typeof window.dispatchEvent === "function") {
    window.dispatchEvent(new PopStateEvent("popstate"));
  }
}

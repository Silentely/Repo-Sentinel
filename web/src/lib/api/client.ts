import type { QueryClient } from "@tanstack/react-query";

import { queryClient as applicationQueryClient } from "../query-client";
import { ApiError } from "./errors";

const csrfCookieName = "reposentinel_csrf";
const csrfHeaderName = "X-CSRF-Token";

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
    return (await response.json()) as T;
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
  if (typeof document === "undefined") {
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
  let payload: unknown;
  try {
    payload = await response.json();
  } catch {
    payload = undefined;
  }

  const record = isRecord(payload) ? payload : {};
  const errorCode = typeof record.error_code === "string" ? record.error_code : "http_error";
  const message =
    typeof record.message === "string" && record.message.trim() !== ""
      ? record.message
      : "服务器暂时无法完成请求，请稍后重试。";
  const details = isRecord(record.details) ? record.details : undefined;

  return new ApiError({ status: response.status, errorCode, message, details });
}

function isRecord(value: unknown): value is Record<string, unknown> {
  return typeof value === "object" && value !== null && !Array.isArray(value);
}

function redirectToLogin(): void {
  if (typeof window === "undefined" || window.location.pathname === "/login") {
    return;
  }
  window.history.replaceState(null, "", "/login");
  window.dispatchEvent(new PopStateEvent("popstate"));
}

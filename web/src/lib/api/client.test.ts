import { QueryClient } from "@tanstack/react-query";
import { describe, expect, it, vi } from "vitest";

import { createApiClient } from "./client";
import { ApiError } from "./errors";

describe("API client", () => {
  it("解析成功的 JSON 响应", async () => {
    const client = createApiClient({
      fetchImpl: vi.fn(async () =>
        new Response(JSON.stringify({ status: "ready" }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        }),
      ),
    });

    await expect(client<{ status: string }>("/health/ready")).resolves.toEqual({
      status: "ready",
    });
  });

  it("把稳定错误体转换为带状态与详情的 ApiError", async () => {
    const client = createApiClient({
      fetchImpl: vi.fn(async () =>
        new Response(
          JSON.stringify({
            error_code: "validation_failed",
            message: "请检查输入。",
            details: { field: "username" },
          }),
          { status: 400, headers: { "Content-Type": "application/json" } },
        ),
      ),
    });

    const thrown = await client("/api/v1/setup").catch((error: unknown) => error);

    expect(thrown).toBeInstanceOf(ApiError);
    expect(thrown).toMatchObject({
      status: 400,
      errorCode: "validation_failed",
      message: "请检查输入。",
      details: { field: "username" },
    });
  });

  it("收到 401 时清空 QueryClient 并触发登录跳转", async () => {
    const queryClient = new QueryClient();
    queryClient.setQueryData(["auth", "session"], { admin: { username: "operator" } });
    const onUnauthorized = vi.fn();
    const client = createApiClient({
      queryClient,
      onUnauthorized,
      fetchImpl: vi.fn(async () =>
        new Response(
          JSON.stringify({ error_code: "unauthorized", message: "需要重新登录。" }),
          { status: 401, headers: { "Content-Type": "application/json" } },
        ),
      ),
    });

    await expect(client("/api/v1/auth/session")).rejects.toBeInstanceOf(ApiError);

    expect(queryClient.getQueryCache().findAll()).toHaveLength(0);
    expect(onUnauthorized).toHaveBeenCalledTimes(1);
  });

  it("POST 时从 Cookie 读取 CSRF token 并发送约定请求头", async () => {
    document.cookie = "reposentinel_csrf=csrf%20token; Path=/";
    let receivedInit: RequestInit | undefined;
    const client = createApiClient({
      fetchImpl: vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
        receivedInit = init;
        return new Response(JSON.stringify({ logged_out: true }), {
          status: 200,
          headers: { "Content-Type": "application/json" },
        });
      }),
    });

    await client("/api/v1/auth/logout", { method: "POST", body: JSON.stringify({}) });

    const headers = new Headers(receivedInit?.headers);
    expect(headers.get("X-CSRF-Token")).toBe("csrf token");
    expect(headers.get("Content-Type")).toBe("application/json");
  });

  it("抛出的错误文本不包含 token 或 password 详情", async () => {
    const client = createApiClient({
      fetchImpl: vi.fn(async () =>
        new Response(
          JSON.stringify({
            error_code: "validation_failed",
            message: "请求未通过校验。",
            details: {
              token: "token-secret-should-not-leak",
              password: "password-secret-should-not-leak",
            },
          }),
          { status: 400, headers: { "Content-Type": "application/json" } },
        ),
      ),
    });

    const thrown = await client("/api/v1/auth/login").catch((error: unknown) => error);
    const text = String(thrown);

    expect(text).not.toContain("token-secret-should-not-leak");
    expect(text).not.toContain("password-secret-should-not-leak");
  });
});

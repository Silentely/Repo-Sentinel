import { describe, expect, it, vi } from "vitest";

import { createApiClient } from "./client";

function jsonResponse(body: unknown, status = 200): Response {
  return new Response(JSON.stringify(body), {
    status,
    headers: { "Content-Type": "application/json" },
  });
}

describe("api client 请求超时", () => {
  it("未显式传入 signal 时注入默认超时 signal", async () => {
    // 弱网下半开的 fetch 会让 mutation 永久 pending；默认 signal 保证请求可失败重试。
    const seen: RequestInit[] = [];
    const fetchImpl = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(init ?? {});
      return jsonResponse({ ok: true });
    });
    const request = createApiClient({ fetchImpl });

    await request("/api/v1/ping");

    expect(seen).toHaveLength(1);
    const init = seen[0] as RequestInit;
    expect(init.signal).toBeInstanceOf(AbortSignal);
    expect(init.signal?.aborted).toBe(false);
  });

  it("调用方显式传入 signal 时不被覆盖", async () => {
    const seen: RequestInit[] = [];
    const fetchImpl = vi.fn(async (_input: RequestInfo | URL, init?: RequestInit) => {
      seen.push(init ?? {});
      return jsonResponse({ ok: true });
    });
    const request = createApiClient({ fetchImpl });
    const controller = new AbortController();

    await request("/api/v1/ping", { signal: controller.signal });

    const init = seen[0] as RequestInit;
    expect(init.signal).toBe(controller.signal);
  });
});

describe("api client 错误解析", () => {
  it("非 JSON 错误响应时回退展示响应原文而非通用文案", async () => {
    const fetchImpl = vi.fn(async () => {
      return new Response("upstream connect error or disconnect/reset before headers. reset reason: connection termination", {
        status: 503,
        headers: { "Content-Type": "text/plain" },
      });
    });
    const request = createApiClient({ fetchImpl });

    await expect(request("/api/v1/ai/test", { method: "POST", body: "{}" })).rejects.toMatchObject({
      message: expect.stringContaining("upstream connect error"),
    });
  });

  it("标准 JSON 错误无 message 时保持通用文案", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ error_code: "boom" }, 500));
    const request = createApiClient({ fetchImpl });

    await expect(request("/api/v1/ping")).rejects.toMatchObject({
      message: "服务器暂时无法完成请求，请稍后重试。",
    });
  });

  it("标准 JSON 错误优先使用后端 message", async () => {
    const fetchImpl = vi.fn(async () => jsonResponse({ error_code: "ai_field_locked", message: "字段已锁定" }, 409));
    const request = createApiClient({ fetchImpl });

    await expect(request("/api/v1/ai/config", { method: "PUT", body: "{}" })).rejects.toMatchObject({
      message: "字段已锁定",
      errorCode: "ai_field_locked",
    });
  });

  it("204 No Content 成功返回 undefined", async () => {
    const fetchImpl = vi.fn(async () => new Response(null, { status: 204 }));
    const request = createApiClient({ fetchImpl });
    const result = await request("/api/v1/logout", { method: "POST" });
    expect(result).toBeUndefined();
  });
});

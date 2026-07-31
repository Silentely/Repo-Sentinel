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

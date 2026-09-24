import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  config: {
    app_id: 123456,
    client_id: "Iv1.servervalue",
    public_base_url: "https://monitor.example.com",
    private_key_path: "/secrets/github-app.pem",
    webhook_url: "https://monitor.example.com/api/v1/webhooks/github",
    private_key_configured: true,
    webhook_secret_configured: true,
    app_id_locked: false,
    client_id_locked: false,
    public_base_url_locked: false,
    private_key_locked: false,
    webhook_secret_locked: false,
  },
  configCalls: 0,
  apiRequest: vi.fn(async (path: string): Promise<unknown> => {
    if (path.startsWith("/api/v1/system/version")) {
      return { version: "0.6.1", commit: "abc", build_time: "2026-09-01T00:00:00Z" };
    }
    if (path.startsWith("/api/v1/github/installations")) {
      return { items: [] };
    }
    fixtures.configCalls += 1;
    // 第二次起服务端值变化：refetch 产出新对象引用，才会真正触发回填 effect。
    return {
      ...fixtures.config,
      client_id: fixtures.configCalls > 1 ? "Iv1.newservervalue" : fixtures.config.client_id,
    };
  }),
}));

vi.mock("../../lib/api/client", () => ({
  apiRequest: fixtures.apiRequest,
}));

import { GitHubPage } from "./github-page";

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <GitHubPage />
    </QueryClientProvider>,
  );
}

describe("GitHubPage 配置表单回填", () => {
  beforeEach(() => {
    fixtures.configCalls = 0;
    vi.useFakeTimers();
    window.history.replaceState(null, "", "/github");
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("配置 refetch 不覆盖未保存编辑", async () => {
    renderPage();
    // 初始查询在微任务中落地：推进 0 并包裹 act 让 React 提交渲染。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const clientID = screen.getByPlaceholderText("Iv1.xxxxxxxx");
    expect(clientID).toHaveValue("Iv1.servervalue");

    fireEvent.change(clientID, { target: { value: "Iv1.editing" } });
    expect(clientID).toHaveValue("Iv1.editing");

    // 窗口重新可见触发配置 refetch（新对象引用）：旧实现会整体回填，
    // 把未保存的编辑覆盖为服务端值。先推进超过 staleTime（10s）使查询过期，可见性变化才会真正重拉配置。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15_000);
    });
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    await act(async () => {
      window.dispatchEvent(new Event("visibilitychange"));
      await vi.advanceTimersByTimeAsync(0);
    });

    // 先确认 refetch 真的发生过（否则本用例不构成回归），再断言未保存编辑未被服务端新值覆盖。
    expect(fixtures.configCalls).toBe(2);
    expect(screen.getByPlaceholderText("Iv1.xxxxxxxx")).toHaveValue("Iv1.editing");
  });
});

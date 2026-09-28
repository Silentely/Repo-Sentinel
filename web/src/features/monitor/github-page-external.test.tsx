import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen } from "@testing-library/react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

// 服务端对「已登记的外部仓」返回 200 + already_registered=true（幂等，不重置同步状态），
// 新建返回 201。提示语必须据此区分，否则会对未发生的事承诺「将进入基线同步」。
const fixtures = vi.hoisted(() => ({
  externalResponse: {} as Record<string, unknown>,
  externalCalls: 0,
  apiRequest: vi.fn(async (path: string): Promise<unknown> => {
    if (path.startsWith("/api/v1/system/version")) {
      return { version: "0.6.1", commit: "abc", build_time: "2026-09-01T00:00:00Z" };
    }
    if (path.startsWith("/api/v1/github/installations")) {
      return { items: [] };
    }
    if (path === "/api/v1/repositories/external") {
      fixtures.externalCalls += 1;
      return fixtures.externalResponse;
    }
    if (path.startsWith("/api/v1/repositories")) {
      return { items: [], page: 1, per_page: 100, total: 0 };
    }
    if (path.startsWith("/api/v1/github/config")) {
      return {
        app_id: 123456,
        client_id: "Iv1.test",
        public_base_url: "https://monitor.example.com",
        private_key_path: "",
        webhook_url: "https://monitor.example.com/api/v1/webhooks/github",
        private_key_configured: true,
        webhook_secret_configured: true,
        app_id_locked: false,
        client_id_locked: false,
        public_base_url_locked: false,
        private_key_locked: false,
        webhook_secret_locked: false,
      };
    }
    return {};
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

async function submitExternal(fullName: string) {
  const input = await screen.findByPlaceholderText("owner/repo");
  fireEvent.change(input, { target: { value: fullName } });
  fireEvent.click(screen.getByRole("button", { name: "添加外部仓" }));
}

describe("GitHubPage 添加外部仓提示", () => {
  beforeEach(() => {
    fixtures.externalCalls = 0;
    window.history.replaceState(null, "", "/github");
  });

  afterEach(() => {
    vi.clearAllMocks();
  });

  it("新建仓库时提示将进入基线同步", async () => {
    fixtures.externalResponse = { id: "repo-new", full_name: "octo/new", type: "external_public", sync_status: "baseline_sync" };
    renderPage();
    await submitExternal("octo/new");
    expect(await screen.findByText("外部公开仓库已登记，将进入基线同步。")).toBeTruthy();
    expect(fixtures.externalCalls).toBe(1);
  });

  it("已登记仓库时提示未变动而不是承诺基线同步", async () => {
    fixtures.externalResponse = {
      id: "repo-dup",
      full_name: "octo/dup",
      type: "external_public",
      sync_status: "active",
      already_registered: true,
    };
    renderPage();
    await submitExternal("octo/dup");
    expect(await screen.findByText("该仓库已在外部公开仓库列表中，同步状态未变动。")).toBeTruthy();
    expect(screen.queryByText("外部公开仓库已登记，将进入基线同步。")).toBeNull();
    expect(fixtures.externalCalls).toBe(1);
  });
});

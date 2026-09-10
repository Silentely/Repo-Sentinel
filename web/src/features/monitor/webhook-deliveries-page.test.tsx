import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  deliveries: {
    items: [
      {
        id: "del-1",
        delivery_id: "gh-del-12345",
        event_type: "push",
        action: "",
        repository_full_name: "acme/repo",
        status: "processed",
        received_at: "2026-08-08T09:00:00Z",
      },
    ],
    page: 1,
    per_page: 20,
    total: 1,
  },
  detail: {
    delivery: {
      id: "del-1",
      delivery_id: "gh-del-12345",
      event_type: "push",
      action: "",
      repository_full_name: "acme/repo",
      status: "processed",
      received_at: "2026-08-08T09:00:00Z",
    },
    payload_json: { ref: "refs/heads/main", commits: [] },
    payload_raw: '{"ref":"refs/heads/main","commits":[]}',
  },
  apiRequest: vi.fn(async (path: string): Promise<unknown> => {
    if (path.includes("/replay")) {
      return { status: "replayed", id: "del-2", delivery_id: "gh-del-12345-replay-1" };
    }
    if (path.endsWith("/del-1")) {
      return fixtures.detail;
    }
    return fixtures.deliveries;
  }),
}));

vi.mock("../../lib/api/client", () => ({
  apiRequest: fixtures.apiRequest,
}));

import { WebhookDeliveriesPage } from "./webhook-deliveries-page";

describe("WebhookDeliveriesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("渲染 Webhook 投递历史列表并支持打开 Inspector 检查载荷与重放", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });

    render(
      <QueryClientProvider client={queryClient}>
        <WebhookDeliveriesPage />
      </QueryClientProvider>
    );

    // 1. 验证列表项呈现
    expect(await screen.findByText("gh-del-12345")).toBeInTheDocument();
    expect(screen.getByText("acme/repo")).toBeInTheDocument();
    const list = screen.getByRole("list");
    expect(within(list).getByText("已处理")).toBeInTheDocument();

    // 2. 点击检查按钮打开 Inspector
    fireEvent.click(screen.getByRole("button", { name: /检查/i }));

    // 3. 验证 Inspector 对话框中展示 JSON 载荷
    expect(await screen.findByText(/Webhook 载荷检查/i)).toBeInTheDocument();
    expect(await screen.findByText(/refs\/heads\/main/i)).toBeInTheDocument();

    // 4. 点击一键重放
    const replayBtn = screen.getByRole("button", { name: /一键重放此 Webhook/i });
    fireEvent.click(replayBtn);

    // 5. 验证发起重放请求
    await waitFor(() => {
      expect(fixtures.apiRequest).toHaveBeenCalledWith(
        "/api/v1/webhook-deliveries/del-1/replay",
        expect.objectContaining({ method: "POST" })
      );
    });
  });
});

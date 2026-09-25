import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  repositories: {
    items: [
      {
        id: "repo-1",
        owner: "acme",
        name: "repo",
        full_name: "acme/repo",
        sync_status: "synced",
      },
    ],
    page: 1,
    per_page: 20,
    total: 1,
  },
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
    if (path.includes("/repositories")) {
      return fixtures.repositories;
    }
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

import { ApiError } from "../../lib/api/errors";
import { WebhookDeliveriesPage } from "./webhook-deliveries-page";

describe("WebhookDeliveriesPage", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("渲染 Webhook 传递历史列表并支持打开 Inspector 检查负载与重放", async () => {
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
    const list = screen.getByRole("list");
    expect(within(list).getByText("acme/repo")).toBeInTheDocument();
    expect(within(list).getByText("已处理")).toBeInTheDocument();

    // 2. 点击检查按钮打开 Inspector
    fireEvent.click(screen.getByRole("button", { name: /检查/i }));

    // 3. 验证 Inspector 对话框中展示 JSON 负载
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

  it("负载拉取失败时提示错误而非「未找到记录详情」", async () => {
    // 旧实现把查询失败落到 else 分支：网络/服务端错误被误报为记录不存在。
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    fixtures.apiRequest.mockImplementation(async (path: string): Promise<unknown> => {
      if (path.includes("/repositories")) {
        return fixtures.repositories;
      }
      if (path.endsWith("/del-1")) {
        throw new ApiError({ status: 500, errorCode: "internal", message: "传递记录读取失败" });
      }
      return fixtures.deliveries;
    });

    render(
      <QueryClientProvider client={queryClient}>
        <WebhookDeliveriesPage />
      </QueryClientProvider>,
    );

    fireEvent.click(await screen.findByRole("button", { name: /检查/i }));
    expect(await screen.findByRole("alert")).toHaveTextContent("无法拉取投递载荷");
    expect(screen.queryByText("未找到记录详情")).not.toBeInTheDocument();
  });

  it("翻页把页码写入 URL，刷新后停留在原页", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    fixtures.apiRequest.mockImplementation(async (path: string): Promise<unknown> => {
      if (path.includes("/repositories")) return fixtures.repositories;
      if (path.endsWith("/del-1")) return fixtures.detail;
      return { ...fixtures.deliveries, total: 45 };
    });
    window.history.replaceState(null, "", "/webhook-deliveries");

    render(
      <QueryClientProvider client={queryClient}>
        <WebhookDeliveriesPage />
      </QueryClientProvider>,
    );

    await screen.findByText("gh-del-12345");
    fireEvent.click(screen.getByRole("button", { name: "下一页" }));
    expect(new URLSearchParams(window.location.search).get("page")).toBe("2");
    expect(await screen.findByText(/第 2 \/ 3 页/)).toBeInTheDocument();
  });

  it("URL 中非法页码安全回退到第 1 页", async () => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    fixtures.apiRequest.mockImplementation(async (path: string): Promise<unknown> => {
      if (path.includes("/repositories")) return fixtures.repositories;
      if (path.endsWith("/del-1")) return fixtures.detail;
      return { ...fixtures.deliveries, total: 45 };
    });
    window.history.replaceState(null, "", "/webhook-deliveries?page=0");

    render(
      <QueryClientProvider client={queryClient}>
        <WebhookDeliveriesPage />
      </QueryClientProvider>,
    );

    await screen.findByText("gh-del-12345");
    expect(screen.getByText(/第 1 \//)).toBeInTheDocument();
  });
});

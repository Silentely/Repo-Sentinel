import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  outbox: {
    items: [
      {
        id: "out-1",
        channel_id: "channel-1",
        channel_type: "telegram",
        status: "dead",
        title: "失败通知",
        attempt_count: 8,
        last_error_code: "telegram_status_500",
        html_url: "https://github.com/example/repo/issues/1",
        created_at: "2026-08-08T09:00:00Z",
        updated_at: "2026-08-08T09:01:00Z",
      },
      {
        id: "out-2",
        channel_id: "channel-1",
        channel_type: "http_webhook",
        status: "dead",
        title: "第二条失败通知",
        attempt_count: 8,
        last_error_code: "http_webhook_status_503",
        html_url: "",
        created_at: "2026-08-08T09:02:00Z",
        updated_at: "2026-08-08T09:03:00Z",
      },
    ],
    page: 1,
    per_page: 50,
    total: 2,
  },
  apiRequest: vi.fn(async (path: string): Promise<unknown> => {
    if (path.includes("/retry")) {
      return undefined;
    }
    return fixtures.outbox;
  }),
}));

vi.mock("../../lib/api/client", () => ({
  apiRequest: fixtures.apiRequest,
}));

import { OutboxPage } from "./outbox-page";

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <OutboxPage />
    </QueryClientProvider>,
  );
}

describe("OutboxPage", () => {
  beforeEach(() => {
    fixtures.apiRequest.mockImplementation(async (path: string) => {
      if (path.includes("/retry")) {
        return undefined;
      }
      return fixtures.outbox;
    });
  });

  it("使用独立详情按钮，避免列表项与重试按钮嵌套交互", async () => {
    renderPage();

    const title = await screen.findByText("失败通知");
    const row = title.closest("li") as HTMLElement;
    expect(row).not.toHaveAttribute("role", "button");
    expect(row.querySelector('[role="button"]')).toBeNull();

    const detailButton = within(row).getByRole("button", { name: "查看投递详情：失败通知" });
    fireEvent.click(detailButton);

    expect(screen.getByRole("dialog", { name: "投递详情" })).toBeInTheDocument();
  });

  it("详情抽屉对已收录错误码展示中文排障提示", async () => {
    renderPage();

    const title = await screen.findByText("第二条失败通知");
    const row = title.closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "查看投递详情：第二条失败通知" }));

    const dialog = screen.getByRole("dialog", { name: "投递详情" });
    expect(within(dialog).getByText("http_webhook_status_503")).toBeInTheDocument();
    expect(within(dialog).getByText(/服务端错误/)).toBeInTheDocument();
  });

  it("批量重试遇到单条失败时仍继续，并反馈成功与失败数量", async () => {
    fixtures.apiRequest.mockImplementation(async (path: string) => {
      if (path.includes("/out-1/retry")) {
        throw new Error("upstream unavailable");
      }
      if (path.includes("/out-2/retry")) {
        return undefined;
      }
      return fixtures.outbox;
    });

    renderPage();
    fireEvent.click(await screen.findByRole("button", { name: "投递失败" }));
    const retryAllButton = await screen.findByRole("button", { name: "重试本页失败 (2)" });
    fireEvent.click(retryAllButton);

    await waitFor(() => {
      expect(fixtures.apiRequest.mock.calls.some(([path]) => path.includes("/out-1/retry"))).toBe(true);
      expect(fixtures.apiRequest.mock.calls.some(([path]) => path.includes("/out-2/retry"))).toBe(true);
    });
    expect(await screen.findByText("已重新排队 1 条，另有 1 条失败；请查看列表中的错误码后重试。"))
      .toBeInTheDocument();
  });

  it("单条重试进行中时标记当前行忙碌并禁用当前按钮", async () => {
    let resolveRetry: () => void = () => undefined;
    const retryPending = new Promise<void>((resolve) => {
      resolveRetry = resolve;
    });
    fixtures.apiRequest.mockImplementation(async (path: string) => {
      if (path.includes("/out-1/retry")) {
        return retryPending;
      }
      return fixtures.outbox;
    });

    renderPage();
    const title = await screen.findByText("失败通知");
    const row = title.closest("li") as HTMLElement;
    const retryButton = within(row).getByRole("button", { name: "重试投递：失败通知" });
    fireEvent.click(retryButton);

    await waitFor(() => expect(row).toHaveAttribute("aria-busy", "true"));
    expect(retryButton).toBeDisabled();
    resolveRetry();
  });
});

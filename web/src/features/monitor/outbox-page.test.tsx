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
        body_text: "<b>🟢 已打开｜修复登录 Bug</b>\n📦 仓库：<code>org/repo</code>",
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

  it("URL 携带 status=dead 时初始进入投递失败筛选（仪表盘跳转直达）", async () => {
    window.history.replaceState(null, "", "/notifications/outbox?status=dead");
    try {
      renderPage();
      expect(await screen.findByRole("button", { name: "重试本页失败 (2)" })).toBeInTheDocument();
    } finally {
      window.history.replaceState(null, "", "/");
    }
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

  it("详情抽屉支持一键复制投递 ID", async () => {
    const writeText = vi.fn(async () => undefined);
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderPage();

    const title = await screen.findByText("失败通知");
    const row = title.closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "查看投递详情：失败通知" }));

    const dialog = screen.getByRole("dialog", { name: "投递详情" });
    fireEvent.click(within(dialog).getByRole("button", { name: "复制投递 ID" }));

    expect(writeText).toHaveBeenCalledWith("out-1");
    expect(await within(dialog).findByText("已复制")).toBeInTheDocument();
  });

  it("详情抽屉以纯文本展示通知正文（剔除 HTML 标签）", async () => {
    renderPage();

    const title = await screen.findByText("失败通知");
    const row = title.closest("li") as HTMLElement;
    fireEvent.click(within(row).getByRole("button", { name: "查看投递详情：失败通知" }));

    const dialog = screen.getByRole("dialog", { name: "投递详情" });
    expect(within(dialog).getByText(/修复登录 Bug/)).toBeInTheDocument();
    expect(within(dialog).getByText(/org\/repo/)).toBeInTheDocument();
    // 不应渲染原始 HTML 标签。
    expect(within(dialog).queryByText(/<b>/)).toBeNull();
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

  it("重试全部失败时跨页收集 dead 投递并逐个重新排队", async () => {
    // 批量操作需要确认：mock confirm 返回 true。
    const confirmSpy = vi.spyOn(window, "confirm").mockReturnValue(true);
    try {
      // 第二页返回空：验证跨页循环在取完时停止，不会死循环。
      fixtures.apiRequest.mockImplementation(async (path: string) => {
        if (path.includes("/out-1/retry") || path.includes("/out-2/retry")) {
          return undefined;
        }
        const isPageTwo = path.includes("page=2");
        return isPageTwo
          ? { items: [], page: 2, per_page: 100, total: 2 }
          : fixtures.outbox;
      });

      renderPage();
      const retryAllButton = await screen.findByRole("button", { name: "重试全部失败 (2)" });
      fireEvent.click(retryAllButton);

      expect(confirmSpy).toHaveBeenCalledWith("确定要重新排队全部 2 条失败投递吗？");
      await waitFor(() => {
        expect(fixtures.apiRequest.mock.calls.some(([path]) => path.includes("/out-1/retry"))).toBe(true);
        expect(fixtures.apiRequest.mock.calls.some(([path]) => path.includes("/out-2/retry"))).toBe(true);
      });
      expect(await screen.findByText("已重新排队 2 条失败投递。")).toBeInTheDocument();
    } finally {
      confirmSpy.mockRestore();
    }
  });
});

import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { NotificationSimulatorDrawer } from "./notification-simulator-drawer";
import * as api from "../api";

describe("NotificationSimulatorDrawer", () => {
  const renderDrawer = (open = true, onClose = vi.fn()) => {
    const queryClient = new QueryClient({
      defaultOptions: { queries: { retry: false } },
    });
    return {
      ...render(
        <QueryClientProvider client={queryClient}>
          <NotificationSimulatorDrawer open={open} onClose={onClose} />
        </QueryClientProvider>
      ),
      onClose,
    };
  };

  it("does not render when open is false", () => {
    renderDrawer(false);
    expect(screen.queryByText("通知效果多渠道模拟器")).not.toBeInTheDocument();
  });

  it("renders presets and triggers dry-run mutation displaying results", async () => {
    const mockDryRun = vi.spyOn(api, "dryRunRules").mockResolvedValue({
      event_kind: "pull_request",
      action: "opened",
      repository: "octocat/Hello-World",
      title: "🟢 已开启｜feat: add super fast notification simulator",
      body_text: "<b>🟢 已开启｜feat: add super fast notification simulator</b>\n📦 仓库：<code>octocat/Hello-World</code>",
      html_url: "https://github.com/octocat/Hello-World/pull/101",
      matched_rules: ["capability_allowed", "realtime_evaluation_pass", "channel:telegram:delivered"],
      channel_results: [
        {
          channel_id: "ch-tg-1",
          channel_type: "telegram",
          name: "Telegram Group",
          matched: true,
          reason: "matched",
          parse_mode: "HTML",
        },
        {
          channel_id: "ch-fs-1",
          channel_type: "feishu",
          name: "Feishu Bot",
          matched: false,
          reason: "channel_disabled",
          parse_mode: "HTML",
        },
      ],
      is_muted: false,
    });

    const { onClose } = renderDrawer(true);

    expect(screen.getByText("通知效果多渠道模拟器")).toBeInTheDocument();
    expect(screen.getByText("纯内存沙盒评估，不产生真实发送与数据库记录")).toBeInTheDocument();

    // Switch preset to issues
    fireEvent.click(screen.getByRole("button", { name: "issues" }));
    expect(screen.getByDisplayValue("issues")).toBeInTheDocument();

    // Switch back to pull_request
    fireEvent.click(screen.getByRole("button", { name: "pull_request" }));
    expect(screen.getByDisplayValue("pull_request")).toBeInTheDocument();

    // Click Run
    const runBtn = screen.getByRole("button", { name: /运行模拟评估/i });
    fireEvent.click(runBtn);

    await waitFor(() => {
      expect(mockDryRun).toHaveBeenCalledTimes(1);
    });

    // Check preview and channels rendered
    await waitFor(() => {
      expect(screen.getByTestId("simulator-results")).toBeInTheDocument();
    });

    expect(screen.getAllByText("🟢 已开启｜feat: add super fast notification simulator").length).toBeGreaterThanOrEqual(1);
    expect(screen.getByText("Telegram Group")).toBeInTheDocument();
    expect(screen.getByText("命中投递")).toBeInTheDocument();
    expect(screen.getByText(/过滤 \(channel_disabled\)/)).toBeInTheDocument();
    expect(screen.getByText("capability_allowed")).toBeInTheDocument();

    // Close button
    const closeBtn = screen.getByRole("button", { name: "关闭" });
    fireEvent.click(closeBtn);
    expect(onClose).toHaveBeenCalled();
  });
});

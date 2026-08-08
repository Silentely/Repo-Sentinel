import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  channels: {
    items: [
      {
        id: "channel-telegram",
        channel_type: "telegram",
        name: "Telegram",
        enabled: true,
        target: "123456",
        secret_configured: true,
        event_kinds: null,
        digest_enabled: true,
      },
      {
        id: "channel-webhook",
        channel_type: "http_webhook",
        name: "HTTP Webhook",
        enabled: true,
        target: "https://hooks.example.com/notify",
        secret_configured: false,
        event_kinds: null,
        digest_enabled: true,
      },
    ],
  },
  settings: {
    "digest.local_time": "09:00",
    "admin.timezone": "UTC",
  },
  apiRequest: vi.fn(async (path: string): Promise<unknown> => {
    if (path.includes("/notifications/channels")) return fixtures.channels;
    if (path.includes("/system/settings")) return fixtures.settings;
    return undefined;
  }),
}));

vi.mock("../../lib/api/client", () => ({
  apiRequest: fixtures.apiRequest,
}));

vi.mock("@tanstack/react-router", () => ({
  Link: ({ children }: { children?: ReactNode }) => <a href="/settings">{children}</a>,
}));

import { NotifyPage } from "./notify-page";

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <NotifyPage />
    </QueryClientProvider>,
  );
}

describe("NotifyPage", () => {
  beforeEach(() => {
    fixtures.apiRequest.mockImplementation(async (path: string) => {
      if (path.includes("/notifications/channels")) return fixtures.channels;
      if (path.includes("/system/settings")) return fixtures.settings;
      return undefined;
    });
  });

  it("测试一个渠道时只显示该渠道发送中，另一渠道仍可操作", async () => {
    let resolveTest: () => void = () => undefined;
    const testPending = new Promise<void>((resolve) => {
      resolveTest = resolve;
    });
    fixtures.apiRequest.mockImplementation(async (path: string) => {
      if (path.includes("/notifications/channels/telegram/test")) return testPending;
      if (path.includes("/notifications/channels")) return fixtures.channels;
      if (path.includes("/system/settings")) return fixtures.settings;
      return undefined;
    });

    renderPage();
    const telegramRow = (await screen.findByText("📱 Telegram", { exact: true })).closest("li") as HTMLElement;
    const webhookRow = (await screen.findByText("🌐 HTTP Webhook", { exact: true })).closest("li") as HTMLElement;
    const telegramForm = screen.getByRole("heading", { name: "Telegram" }).closest("section") as HTMLElement;
    const webhookForm = screen.getByRole("heading", { name: "HTTP Webhook" }).closest("section") as HTMLElement;

    fireEvent.click(within(telegramRow).getByRole("button", { name: "测试" }));

    await waitFor(() => expect(within(telegramRow).getByRole("button", { name: "发送中…" })).toBeDisabled());
    expect(within(webhookRow).getByRole("button", { name: "测试" })).toBeEnabled();
    expect(within(telegramForm).getByRole("button", { name: "发送中…" })).toBeDisabled();
    expect(within(webhookForm).getByRole("button", { name: "🔔 发送测试通知" })).toBeEnabled();
    resolveTest();
  });
});

import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

// 页面内链接与真实路由解耦：渲染为普通锚点即可。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

// 共享设置数据：覆盖全部保存键，便于断言参考值进入提交负载。
const settingsFixture = {
  "admin.timezone": "Asia/Shanghai",
  "digest.local_time": "08:30",
  "digest.send_empty": true,
  "notify.aggregate_window_sec": 120,
  "notify.burst_threshold": 30,
  "notify.burst_window_sec": 300,
  "display.closed_limit": 50,
  "retention.events_days": 60,
  "retention.outbox_days": 15,
  "retention.webhook_deliveries_days": 10,
  "feature.issues": true,
  "feature.pull_requests": true,
  "feature.actions": true,
  "feature.security_alerts": true,
};

const { saveSystemSettingsMock } = vi.hoisted(() => ({
  saveSystemSettingsMock: vi.fn(async (body: Record<string, unknown>) => body),
}));

vi.mock("./api", () => ({
  settingsQueryOptions: {
    queryKey: ["test", "settings"],
    queryFn: async () => settingsFixture,
  },
  // 版本查询保持 pending：测试聚焦保存反馈，避免无关请求干扰。
  versionQueryOptions: {
    queryKey: ["test", "version"],
    queryFn: () => new Promise<never>(() => undefined),
  },
  checkForUpdates: vi.fn(async () => ({})),
  saveSystemSettings: saveSystemSettingsMock,
}));

vi.mock("../auth/api", () => ({
  changePassword: vi.fn(async () => undefined),
}));

import { AboutPage } from "./about-page";

function renderPage() {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <AboutPage />
    </QueryClientProvider>,
  );
}

describe("关于与设置页", () => {
  beforeEach(() => {
    saveSystemSettingsMock.mockClear();
  });

  it("保存开关成功后在功能模块区块内展示提示", async () => {
    const user = userEvent.setup();
    renderPage();

    // 等待设置回填后关闭 Issues，验证开关状态进入提交负载。
    const issuesToggle = await screen.findByRole("checkbox", { name: "Issues" });
    expect(issuesToggle).toBeChecked();
    await user.click(issuesToggle);

    await user.click(screen.getByRole("button", { name: "保存开关" }));

    const featuresSection = screen.getByRole("region", { name: "功能模块开关" });
    expect(await within(featuresSection).findByText("功能模块开关已保存。")).toBeInTheDocument();
    // 提示不得落在不可见的运行偏好区块。
    const prefsSection = screen.getByRole("region", { name: "运行偏好" });
    expect(within(prefsSection).queryByText("功能模块开关已保存。")).toBeNull();

    // 功能区块只提交 feature.*，避免覆盖运行偏好。
    expect(saveSystemSettingsMock).toHaveBeenCalledWith({
      "feature.issues": false,
      "feature.pull_requests": true,
      "feature.actions": true,
      "feature.security_alerts": true,
    });
  });

  it("保存偏好成功后在运行偏好区块内展示提示", async () => {
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole("checkbox", { name: "Issues" });
    await user.click(screen.getByRole("button", { name: "保存偏好" }));

    const prefsSection = screen.getByRole("region", { name: "运行偏好" });
    expect(await within(prefsSection).findByText("系统偏好已保存。")).toBeInTheDocument();
    expect(saveSystemSettingsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        "admin.timezone": "Asia/Shanghai",
        "digest.local_time": "08:30",
        "notify.burst_window_sec": 300,
      }),
    );
    const prefsPayload = saveSystemSettingsMock.mock.calls[0]?.[0] as Record<string, unknown> | undefined;
    expect(prefsPayload).toBeDefined();
    expect(prefsPayload).not.toHaveProperty("feature.issues");
  });

  it("保存失败时在功能模块区块内展示错误且不出现成功提示", async () => {
    saveSystemSettingsMock.mockRejectedValueOnce(new Error("boom"));
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole("checkbox", { name: "Issues" });
    await user.click(screen.getByRole("button", { name: "保存开关" }));

    const featuresSection = screen.getByRole("region", { name: "功能模块开关" });
    expect(await within(featuresSection).findByText("保存失败")).toBeInTheDocument();
    expect(within(featuresSection).queryByText("功能模块开关已保存。")).toBeNull();
  });
});

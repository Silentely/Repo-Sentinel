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
  "report.weekly_enabled": true,
  "report.weekly_day": "friday",
  "report.monthly_enabled": false,
  "report.monthly_day": 1,
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

const { saveSystemSettingsMock, saveAIConfigMock } = vi.hoisted(() => ({
  saveSystemSettingsMock: vi.fn(async (body: Record<string, unknown>) => body),
  saveAIConfigMock: vi.fn(async (body: Record<string, unknown>) => body),
}));

// AI 配置初始快照：全部字段可编辑（unset 来源）、密钥未配置。
const aiConfigFixture = {
  enabled: false,
  base_url: "https://api.openai.com/v1",
  model: "gpt-4o-mini",
  timeout_sec: 20,
  max_tokens: 800,
  digest_enabled: true,
  triage_enabled: true,
  api_key_configured: false,
  enabled_source: "unset",
  base_url_source: "unset",
  model_source: "unset",
  timeout_source: "unset",
  max_tokens_source: "unset",
  api_key_source: "unset",
  digest_enabled_source: "unset",
  triage_enabled_source: "unset",
  enabled_locked: false,
  base_url_locked: false,
  model_locked: false,
  timeout_locked: false,
  max_tokens_locked: false,
  api_key_locked: false,
  digest_enabled_locked: false,
  triage_enabled_locked: false,
  can_edit_in_ui: true,
  note: "",
};

vi.mock("./api", () => ({
  settingsQueryOptions: {
    queryKey: ["test", "settings"],
    queryFn: async () => settingsFixture,
  },
  aiConfigQueryOptions: {
    queryKey: ["test", "ai-config"],
    queryFn: async () => aiConfigFixture,
  },
  // 版本查询保持 pending：测试聚焦保存反馈，避免无关请求干扰。
  versionQueryOptions: {
    queryKey: ["test", "version"],
    queryFn: () => new Promise<never>(() => undefined),
  },
  checkForUpdates: vi.fn(async () => ({})),
  saveSystemSettings: saveSystemSettingsMock,
  saveAIConfig: saveAIConfigMock,
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
        "report.weekly_enabled": true,
        "report.weekly_day": "friday",
        "report.monthly_enabled": false,
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

  it("保存 AI 配置时提交表单字段且不回显密钥", async () => {
    const user = userEvent.setup();
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    await user.click(within(aiSection).getByRole("checkbox", { name: /启用 AI/ }));
    const modelInput = within(aiSection).getByLabelText("模型");
    await user.clear(modelInput);
    await user.type(modelInput, "llama3.1");
    const keyInput = within(aiSection).getByLabelText(/留空保持不变/);
    await user.type(keyInput, "sk-ui-key");
    await user.click(within(aiSection).getByRole("button", { name: "保存 AI 配置" }));

    expect(await within(aiSection).findByText("AI 配置已保存。")).toBeInTheDocument();
    expect(saveAIConfigMock).toHaveBeenCalledWith(
      expect.objectContaining({
        enabled: true,
        model: "llama3.1",
        api_key: "sk-ui-key",
      }),
    );
    const payload = saveAIConfigMock.mock.calls[0]?.[0] as Record<string, unknown> | undefined;
    expect(payload).toBeDefined();
    // 全字段可编辑时提交完整配置（timeout/max_tokens 为数值）。
    expect(payload?.timeout_sec).toBe(20);
    expect(payload?.max_tokens).toBe(800);
  });
});

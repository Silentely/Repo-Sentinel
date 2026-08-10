import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, waitFor, within } from "@testing-library/react";
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
  "feature.stars": true,
  "feature.watches": true,
};

const { saveSystemSettingsMock, saveAIConfigMock, testAIConnectivityMock, activateRepositoryMock, reconcileAllMock, reconcileRepositoryMock } = vi.hoisted(() => ({
  saveSystemSettingsMock: vi.fn(async (body: Record<string, unknown>) => body),
  saveAIConfigMock: vi.fn(async (body: Record<string, unknown>) => body),
  testAIConnectivityMock: vi.fn(
    async (body: Record<string, unknown>): Promise<{ ok: boolean; message: string; model: string; base_url: string; latency_ms: number }> => ({
      ok: true,
      message: "连通性测试成功：模型 gpt-4o-mini 正常回复（42 ms）",
      model: "gpt-4o-mini",
      base_url: "https://api.openai.com/v1",
      latency_ms: 42,
    }),
  ),
  activateRepositoryMock: vi.fn(async (_id: string) => ({})),
  reconcileAllMock: vi.fn(async () => {}),
  reconcileRepositoryMock: vi.fn(async (_id: string) => {}),
}));

// 仓库对账区块数据：一个 active（放行完成）与一个 baseline（待放行）。
const reposFixture = {
  items: [
    {
      id: "repo-1",
      type: "github_installation",
      sync_status: "active",
      full_name: "owner/repo-a",
      owner: "owner",
      name: "repo-a",
      is_private: true,
      is_archived: false,
      html_url: "https://github.com/owner/repo-a",
    },
    {
      id: "repo-2",
      type: "github_installation",
      sync_status: "baseline_sync",
      full_name: "owner/repo-b",
      owner: "owner",
      name: "repo-b",
      is_private: true,
      is_archived: false,
      html_url: "https://github.com/owner/repo-b",
    },
  ],
  page: 1,
  per_page: 100,
  total: 2,
};

// AI 配置初始快照：全部字段可编辑（unset 来源）、密钥未配置。
// 用 let 以便单测覆盖「后端未设置标量字段（空串 / 0）」的回退场景。
let aiConfigFixture = {
  enabled: false,
  base_url: "https://api.openai.com/v1",
  model: "gpt-4o-mini",
  timeout_sec: 20,
  max_tokens: 800,
  retries: 1,
  digest_enabled: true,
  triage_enabled: true,
  api_key_configured: false,
  enabled_source: "unset",
  base_url_source: "unset",
  model_source: "unset",
  timeout_source: "unset",
  max_tokens_source: "unset",
  retries_source: "unset",
  api_key_source: "unset",
  digest_enabled_source: "unset",
  triage_enabled_source: "unset",
  enabled_locked: false,
  base_url_locked: false,
  model_locked: false,
  timeout_locked: false,
  max_tokens_locked: false,
  retries_locked: false,
  api_key_locked: false,
  digest_enabled_locked: false,
  triage_enabled_locked: false,
  can_edit_in_ui: true,
  note: "",
};

// 供单个用例模拟 AI 配置查询失败（如反代瞬时故障），验证保存/测试按钮被禁用。
let aiConfigQueryError: Error | null = null;

vi.mock("./api", () => ({
  settingsQueryOptions: {
    queryKey: ["test", "settings"],
    queryFn: async () => settingsFixture,
  },
  aiConfigQueryOptions: {
    queryKey: ["test", "ai-config"],
    queryFn: async () => {
      if (aiConfigQueryError) {
        throw aiConfigQueryError;
      }
      return aiConfigFixture;
    },
  },
  repositoriesQueryOptions: {
    queryKey: ["test", "repos"],
    queryFn: async () => reposFixture,
  },
  starredReleasesConfigQueryOptions: {
    queryKey: ["test", "starred-releases-config"],
    queryFn: async () => ({
      username: "",
      star_sync_interval: "6h0m0s",
      release_poll_interval: "10m0s",
      max_trackers: 500,
      notify_prerelease: false,
      enabled: true,
      ai_release_summary_enabled: true,
      counts: { tracking: 0, inactive: 0, disabled: 0, unavailable: 0 },
    }),
  },
  listStarredTrackers: vi.fn(async () => ({ items: [], page: 1, per_page: 20, total: 0 })),
  saveStarredReleasesConfig: vi.fn(async (body: Record<string, unknown>) => body),
  syncStarredReleases: vi.fn(async () => ({ started: false })),
  setStarredTrackerState: vi.fn(async () => ({ ok: true })),
  saveSystemSettings: saveSystemSettingsMock,
  saveAIConfig: saveAIConfigMock,
  testAIConnectivity: testAIConnectivityMock,
  activateRepository: activateRepositoryMock,
  reconcileAll: reconcileAllMock,
  reconcileRepository: reconcileRepositoryMock,
}));

vi.mock("../auth/api", () => ({
  changePassword: vi.fn(async () => undefined),
}));

import { SettingsPage } from "./settings-page";

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <SettingsPage />
    </QueryClientProvider>,
  );
}

describe("设置页", () => {
  beforeEach(() => {
    saveSystemSettingsMock.mockClear();
    testAIConnectivityMock.mockClear();
    activateRepositoryMock.mockClear();
    reconcileAllMock.mockClear();
    reconcileRepositoryMock.mockClear();
  });

  it("时区输入失焦时即时校验：非法值提示、合法值静默", async () => {
    const user = userEvent.setup();
    renderPage();

    const prefsSection = screen.getByRole("region", { name: "运行偏好" });
    const timezoneInput = await within(prefsSection).findByLabelText("管理员时区");
    // 等待设置回填（表单初值来自默认，查询落定后更新为 Asia/Shanghai）。
    await waitFor(() => expect(timezoneInput).toHaveValue("Asia/Shanghai"));

    // 非法时区：失焦后提示，保存后后端仍会强校验。
    await user.clear(timezoneInput);
    await user.type(timezoneInput, "Mars/Olympus");
    await user.tab();
    expect(await within(prefsSection).findByText(/无法识别的时区/)).toBeInTheDocument();

    // 合法时区：提示消失。
    await user.clear(timezoneInput);
    await user.type(timezoneInput, "Asia/Shanghai");
    await user.tab();
    await waitFor(() => {
      expect(within(prefsSection).queryByText(/无法识别的时区/)).toBeNull();
    });
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
      "feature.stars": true,
      "feature.watches": true,
    });
  });

  it("功能模块区块提供 Star 事件与 Watch 事件开关并提交对应键", async () => {
    const user = userEvent.setup();
    renderPage();

    // 等待设置回填后两个新开关默认开启。
    const starToggle = await screen.findByRole("checkbox", { name: "Star 事件" });
    const watchToggle = screen.getByRole("checkbox", { name: "Watch 事件" });
    expect(starToggle).toBeChecked();
    expect(watchToggle).toBeChecked();

    // 关闭 Star 事件后保存，验证两个键进入提交负载。
    await user.click(starToggle);
    await user.click(screen.getByRole("button", { name: "保存开关" }));

    const featuresSection = screen.getByRole("region", { name: "功能模块开关" });
    expect(await within(featuresSection).findByText("功能模块开关已保存。")).toBeInTheDocument();
    expect(saveSystemSettingsMock).toHaveBeenCalledWith(
      expect.objectContaining({
        "feature.stars": false,
        "feature.watches": true,
      }),
    );
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
    // 全字段可编辑时提交完整配置（timeout/max_tokens/retries 为数值）。
    expect(payload?.timeout_sec).toBe(20);
    expect(payload?.max_tokens).toBe(800);
    expect(payload?.retries).toBe(1);
  });

  it("重试次数输入框可修改并随保存提交（0 表示不重试）", async () => {
    const user = userEvent.setup();
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    const retriesInput = within(aiSection).getByLabelText("重试次数");
    // 默认值与后端 DefaultRetries 一致。
    expect(retriesInput).toHaveValue(1);
    // 等 AI 配置查询落定（保存按钮随 isLoading 解禁），避免后续输入被数据回填的
    // useEffect 重置覆盖——真实用户操作时表单早已加载完成。
    await waitFor(() => {
      expect(within(aiSection).getByRole("button", { name: "保存 AI 配置" })).toBeEnabled();
    });
    // number input 用 change 直接设值，规避 jsdom 中 clear 后光标位置导致的字符拼接。
    fireEvent.change(retriesInput, { target: { value: "3" } });
    await user.click(within(aiSection).getByRole("button", { name: "保存 AI 配置" }));

    expect(await within(aiSection).findByText("AI 配置已保存。")).toBeInTheDocument();
    expect(saveAIConfigMock).toHaveBeenCalledWith(expect.objectContaining({ retries: 3 }));

    // 0（不重试）为合法显式值。
    fireEvent.change(retriesInput, { target: { value: "0" } });
    await user.click(within(aiSection).getByRole("button", { name: "保存 AI 配置" }));
    expect(saveAIConfigMock).toHaveBeenLastCalledWith(expect.objectContaining({ retries: 0 }));
  });

  it("重试次数被环境变量锁定时输入框禁用", async () => {
    const original = aiConfigFixture;
    aiConfigFixture = { ...original, retries_locked: true };
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    const retriesInput = within(aiSection).getByLabelText("重试次数");
    // 禁用状态依赖 AI 配置查询结果，等待数据落定后再断言。
    await waitFor(() => expect(retriesInput).toBeDisabled());

    aiConfigFixture = original;
  });

  it("后端未设置标量字段（空串 / 0）时表单回退显示默认值", async () => {
    const original = aiConfigFixture;
    // 模拟修复后的后端：未显式配置的字段返回空串 / 0（此前因默认值被误锁，永远返回默认值）。
    aiConfigFixture = { ...original, base_url: "", model: "", timeout_sec: 0, max_tokens: 0 };
    try {
      const user = userEvent.setup();
      renderPage();

      const aiSection = await screen.findByRole("region", { name: "AI 集成" });
      expect(within(aiSection).getByLabelText("API Base URL")).toHaveValue("https://api.openai.com/v1");
      expect(within(aiSection).getByLabelText("模型")).toHaveValue("gpt-4o-mini");
      expect(within(aiSection).getByLabelText("请求超时（秒）")).toHaveValue(20);
      expect(within(aiSection).getByLabelText("输出 token 上限")).toHaveValue(800);

      // 未修改直接保存时，不得把 0/空串提交给后端（会触发 400 校验失败）。
      await user.click(within(aiSection).getByRole("button", { name: "保存 AI 配置" }));
      await within(aiSection).findByText("AI 配置已保存。");
      const payload = saveAIConfigMock.mock.calls[0]?.[0] as Record<string, unknown> | undefined;
      expect(payload?.timeout_sec).toBe(20);
      expect(payload?.max_tokens).toBe(800);
    } finally {
      aiConfigFixture = original;
    }
  });

  it("点击测试连通性成功时展示成功提示并携带表单值", async () => {
    const user = userEvent.setup();
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    await user.click(within(aiSection).getByRole("button", { name: /测试连通性/ }));

    expect(await within(aiSection).findByText(/连通性测试成功/)).toBeInTheDocument();
    expect(testAIConnectivityMock).toHaveBeenCalledWith(
      expect.objectContaining({ model: "gpt-4o-mini", timeout_sec: 20, max_tokens: 800 }),
    );
  });

  it("连通性测试失败（ok=false）时展示错误提示", async () => {
    testAIConnectivityMock.mockResolvedValueOnce({
      ok: false,
      message: "连通性测试失败：ai: http 401",
      model: "gpt-4o-mini",
      base_url: "https://api.openai.com/v1",
      latency_ms: 0,
    });
    const user = userEvent.setup();
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    await user.click(within(aiSection).getByRole("button", { name: /测试连通性/ }));

    expect(await within(aiSection).findByText("连通性测试失败")).toBeInTheDocument();
    expect(within(aiSection).getByText(/ai: http 401/)).toBeInTheDocument();
  });

  it("AI 配置加载失败时禁用保存与测试按钮，避免覆盖已有配置", async () => {
    aiConfigQueryError = new Error("boom");
    try {
      renderPage();

      const aiSection = await screen.findByRole("region", { name: "AI 集成" });
      // 查询失败后表单持有默认回退值，此时保存/测试必须禁用，防止用默认值覆盖 DB 有效配置。
      expect(await within(aiSection).findByText("无法加载 AI 配置")).toBeInTheDocument();
      expect(within(aiSection).getByRole("button", { name: "保存 AI 配置" })).toBeDisabled();
      expect(within(aiSection).getByRole("button", { name: /测试连通性/ })).toBeDisabled();
    } finally {
      aiConfigQueryError = null;
    }
  });

  it("AI 区块操作按钮使用与通知渠道一致的按钮容器布局", async () => {
    renderPage();

    const aiSection = await screen.findByRole("region", { name: "AI 集成" });
    const container = aiSection.querySelector(".channel-form__buttons");
    expect(container).not.toBeNull();
    expect(within(container as HTMLElement).getByRole("button", { name: "保存 AI 配置" })).toBeInTheDocument();
    expect(within(container as HTMLElement).getByRole("button", { name: /测试连通性/ })).toBeInTheDocument();
  });

  it("仓库与基线对账区块展示仓库与同步状态标签", async () => {
    renderPage();

    const reposSection = await screen.findByRole("region", { name: "仓库与基线对账" });
    await within(reposSection).findByText("owner/repo-a");
    expect(within(reposSection).getByText("owner/repo-b")).toBeInTheDocument();
    // 状态标签与「· 自有」同处一个 span；页面提示文案也含「基线中」，需限定在状态行内断言。
    expect(within(reposSection).getAllByText(/正常/, { selector: ".repo-baseline-row__meta" })).toHaveLength(1);
    expect(within(reposSection).getByText(/基线中/, { selector: ".repo-baseline-row__meta" })).toBeInTheDocument();
  });

  it("仓库与基线对账面板可折叠：收起卸载列表，展开恢复，标题行操作按钮常驻", async () => {
    const user = userEvent.setup();
    renderPage();

    const reposSection = await screen.findByRole("region", { name: "仓库与基线对账" });
    await within(reposSection).findByText("owner/repo-a");

    // 点击标题行收起：CollapsiblePanel 仅在展开时渲染 body，仓库列表应卸载。
    await user.click(within(reposSection).getByRole("button", { name: "仓库与基线对账" }));
    await waitFor(() => {
      expect(screen.queryByText("owner/repo-a")).toBeNull();
    });
    // 收起后标题行的「立即对账全部自有仓」仍在，随时可触发对账。
    expect(screen.getByRole("button", { name: "立即对账全部自有仓" })).toBeInTheDocument();

    // 再次点击展开：列表恢复渲染。
    await user.click(screen.getByRole("button", { name: "仓库与基线对账" }));
    await waitFor(() => {
      expect(screen.getByText("owner/repo-a")).toBeInTheDocument();
    });
  });

  it("「立即对账全部自有仓」触发 reconcileAll", async () => {
    const user = userEvent.setup();
    renderPage();

    const reposSection = await screen.findByRole("region", { name: "仓库与基线对账" });
    await user.click(within(reposSection).getByRole("button", { name: "立即对账全部自有仓" }));

    await waitFor(() => expect(reconcileAllMock).toHaveBeenCalledTimes(1));
  });

  it("基线仓库显示「立即放行」，点击触发 activateRepository", async () => {
    const user = userEvent.setup();
    renderPage();

    const reposSection = await screen.findByRole("region", { name: "仓库与基线对账" });
    await within(reposSection).findByText("owner/repo-b");
    await user.click(within(reposSection).getByRole("button", { name: "立即放行" }));

    // mutationFn 第二参为 React Query 上下文，真实函数忽略之，只断言首个实参。
    await waitFor(() => expect(activateRepositoryMock.mock.calls[0]?.[0]).toBe("repo-2"));
  });

  it("单仓「对账」按钮触发 reconcileRepository（外部仓不显示）", async () => {
    const user = userEvent.setup();
    renderPage();

    const reposSection = await screen.findByRole("region", { name: "仓库与基线对账" });
    const rows = await within(reposSection).findAllByText(/owner\/repo-/);
    const row = rows[0]!.closest("li");
    if (!row) {
      throw new Error("未找到仓库行");
    }
    await user.click(within(row as HTMLElement).getByRole("button", { name: "对账" }));

    await waitFor(() => expect(reconcileRepositoryMock.mock.calls[0]?.[0]).toBe("repo-1"));
  });
});

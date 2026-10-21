import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen, waitFor } from "@testing-library/react";
import { beforeEach, describe, expect, it, vi } from "vitest";
import { JevSettingsCard } from "./jev-settings-card";
import { ApiError } from "../../lib/api/errors";
import type { JevConfigDTO } from "./api";

const fakeJevConfig: JevConfigDTO = {
  enabled: true,
  base_url: "https://api.typesafe.ai",
  has_api_key: true,
  api_key_source: "db",
  model: "jev-1.14.0",
  mode: "shadow",
  retries: 1,
  noise_filter_enabled: true,
  routing_enabled: true,
  cascade_gate_enabled: true,
  chatops_guardrail_enabled: true,
  allow_private_state: false,
  notification: {
    p50_budget_ms: 120,
    p95_budget_ms: 400,
    max_concurrency: 25,
    cache_ttl_sec: 3600,
    circuit_fail_count: 5,
    circuit_reset_sec: 30,
  },
  review: {
    p50_budget_ms: 500,
    p95_budget_ms: 900,
    max_concurrency: 10,
    cache_ttl_sec: 3600,
    circuit_fail_count: 5,
    circuit_reset_sec: 30,
  },
  chatops: {
    p50_budget_ms: 100,
    p95_budget_ms: 300,
    max_concurrency: 8,
    cache_ttl_sec: 0,
    circuit_fail_count: 5,
    circuit_reset_sec: 30,
  },
};

const fetchMock = vi.fn();
const updateMock = vi.fn();
const testMock = vi.fn();

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    fetchJevConfig: () => fetchMock(),
    updateJevConfig: (payload: any) => updateMock(payload),
    testJevConfig: () => testMock(),
    jevConfigQueryOptions: {
      queryKey: ["system", "jev"],
      queryFn: () => fetchMock(),
    },
  };
});

function renderCard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <JevSettingsCard />
    </QueryClientProvider>,
  );
}

describe("JevSettingsCard", () => {
  beforeEach(() => {
    fetchMock.mockReset();
    updateMock.mockReset();
    testMock.mockReset();
    fetchMock.mockResolvedValue(fakeJevConfig);
    updateMock.mockResolvedValue(undefined);
    testMock.mockResolvedValue({ status: "ok", model: "jev-1.14.0", input_tokens: 15, output_tokens: 2 });
  });

  it("正确渲染服务端配置并在 API Key 已配置时脱敏显示", async () => {
    renderCard();

    expect(await screen.findByDisplayValue("https://api.typesafe.ai")).toBeInTheDocument();
    expect(screen.getByPlaceholderText("已配置（留空保持原值）")).toBeInTheDocument();
    expect(screen.getByRole("checkbox", { name: "启用 Jev 决策引擎" })).toBeChecked();
    expect(screen.getByRole("combobox", { name: "运行模式" })).toHaveValue("shadow");

    // 验证能力开关渲染
    expect(screen.getByRole("checkbox", { name: /通知智能降噪/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /通知 Incident 标签与分诊路由/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /PR 文档变更快速放行门控/ })).toBeChecked();
    expect(screen.getByRole("checkbox", { name: /ChatOps 指令风险护栏/ })).toBeChecked();
  });

  it("保存修改并发起 PUT 请求", async () => {
    renderCard();

    const urlInput = await screen.findByDisplayValue("https://api.typesafe.ai");
    fireEvent.change(urlInput, { target: { value: "https://proxy.example.com" } });

    // 勾选私有仓外发确认
    const privateStateCb = screen.getByRole("checkbox", { name: /允许私有仓库脱敏元数据外发/ });
    fireEvent.click(privateStateCb);

    const saveBtn = screen.getByRole("button", { name: "保存 Jev 设置" });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalled();
    });

    const payload = updateMock.mock.calls[0]![0];
    expect(payload.base_url).toBe("https://proxy.example.com");
    expect(payload.allow_private_state).toBe(true);
    expect(screen.getByText("Jev 配置已保存")).toBeInTheDocument();
  });

  it("支持明确清除已持久化的 API Key", async () => {
    renderCard();

    await screen.findByDisplayValue("https://api.typesafe.ai");
    const clearCb = screen.getByRole("checkbox", { name: "清除现有 API Key" });
    fireEvent.click(clearCb);

    const saveBtn = screen.getByRole("button", { name: "保存 Jev 设置" });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    await waitFor(() => {
      expect(updateMock).toHaveBeenCalled();
    });

    const payload = updateMock.mock.calls[0]![0];
    expect(payload.clear_api_key).toBe(true);
  });

  it("当切换为 active 且后端拒绝时保留用户输入并提示错误", async () => {
    updateMock.mockRejectedValue(
      new ApiError({
        status: 400,
        errorCode: "activation_gate_failed",
        message: "active mode requires passing all activation gates",
      }),
    );
    renderCard();

    await screen.findByDisplayValue("https://api.typesafe.ai");
    const modeSelect = screen.getByRole("combobox", { name: "运行模式" });
    fireEvent.change(modeSelect, { target: { value: "active" } });
    expect(modeSelect).toHaveValue("active");

    const saveBtn = screen.getByRole("button", { name: "保存 Jev 设置" });
    await act(async () => {
      fireEvent.click(saveBtn);
    });

    await waitFor(() => {
      expect(screen.getByText(/active mode requires passing all activation gates/)).toBeInTheDocument();
    });
    // 用户输入保持为 active
    expect(modeSelect).toHaveValue("active");
  });

  it("运行连通性测试并展示成功提示", async () => {
    renderCard();

    await screen.findByDisplayValue("https://api.typesafe.ai");
    const testBtn = screen.getByRole("button", { name: "测试连通性" });
    await act(async () => {
      fireEvent.click(testBtn);
    });

    await waitFor(() => {
      expect(testMock).toHaveBeenCalled();
      expect(screen.getByText(/连通性测试通过（模型: jev-1.14.0，输入: 15 tokens，输出: 2 tokens）/)).toBeInTheDocument();
    });
  });
});

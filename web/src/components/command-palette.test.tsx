import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, fireEvent, waitFor } from "@testing-library/react";
import { describe, expect, it, vi, beforeEach } from "vitest";

vi.mock("@tanstack/react-router", () => ({
  useNavigate: () => vi.fn(),
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

vi.mock("../features/monitor/api", () => ({
  reconcileAll: vi.fn(async () => {}),
  retryAllDeadOutbox: vi.fn(async () => 5),
}));

import { CommandPalette } from "./command-palette";
import { reconcileAll, retryAllDeadOutbox } from "../features/monitor/api";

function renderPalette(props: { open: boolean; onClose: () => void }) {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <CommandPalette {...props} />
    </QueryClientProvider>,
  );
}

describe("CommandPalette 全局指令面板", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("open 为 false 时不渲染任何内容", () => {
    const onClose = vi.fn();
    const { container } = renderPalette({ open: false, onClose });
    expect(container.firstChild).toBeNull();
  });

  it("open 为 true 时渲染搜索输入框与常用导航指令", () => {
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });

    expect(screen.getByPlaceholderText(/输入指令或搜索/)).toBeInTheDocument();
    expect(screen.getByText("仪表盘")).toBeInTheDocument();
    expect(screen.getByText("Issues")).toBeInTheDocument();
    expect(screen.getByText("Pull Requests")).toBeInTheDocument();
  });

  it("支持根据关键词筛选指令", () => {
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });

    const input = screen.getByPlaceholderText(/输入指令或搜索/);
    fireEvent.change(input, { target: { value: "安全" } });

    expect(screen.getByText("安全告警")).toBeInTheDocument();
    expect(screen.queryByText("Pull Requests")).not.toBeInTheDocument();
  });

  it("破坏性动作（强制全量对账）必须触发二次确认，禁止直接调用 API", async () => {
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });

    const input = screen.getByPlaceholderText(/输入指令或搜索/);
    fireEvent.change(input, { target: { value: "全量对账" } });

    const reconcileItem = screen.getByText("强制全量对账");
    fireEvent.click(reconcileItem);

    // 未确认前绝对不可调用 reconcileAll
    expect(reconcileAll).not.toHaveBeenCalled();

    // 应展示二次确认危险提示
    expect(screen.getByText(/二次确认/)).toBeInTheDocument();
    expect(screen.getByText(/该操作将对所有活跃仓库触发并发全量同步/)).toBeInTheDocument();

    // 点击确认执行后真正触发
    const confirmBtn = screen.getByRole("button", { name: "确认执行" });
    fireEvent.click(confirmBtn);

    await waitFor(() => {
      expect(reconcileAll).toHaveBeenCalledTimes(1);
    });
  });

  it("破坏性动作（重试全部死信）支持在二次确认中取消并返回", async () => {
    const onClose = vi.fn();
    renderPalette({ open: true, onClose });

    const input = screen.getByPlaceholderText(/输入指令或搜索/);
    fireEvent.change(input, { target: { value: "重试" } });

    const retryItem = screen.getByText("重试全部死信投递");
    fireEvent.click(retryItem);

    expect(retryAllDeadOutbox).not.toHaveBeenCalled();
    expect(screen.getByText(/二次确认/)).toBeInTheDocument();

    // 取消返回
    const cancelBtn = screen.getByRole("button", { name: "取消" });
    fireEvent.click(cancelBtn);

    expect(retryAllDeadOutbox).not.toHaveBeenCalled();
    expect(screen.queryByText(/二次确认/)).not.toBeInTheDocument();
    expect(screen.getByPlaceholderText(/输入指令或搜索/)).toBeInTheDocument();
  });
});

import { renderHook } from "@testing-library/react";
import { useGlobalHotkeys } from "./command-palette";

describe("useGlobalHotkeys 全局快捷键", () => {
  it("按下 Cmd+K 或 Ctrl+K 时触发 onTogglePalette", () => {
    const onTogglePalette = vi.fn();
    const onNavigate = vi.fn();
    renderHook(() => useGlobalHotkeys({ onTogglePalette, onNavigate }));

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", metaKey: true }));
    expect(onTogglePalette).toHaveBeenCalledTimes(1);

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "k", ctrlKey: true }));
    expect(onTogglePalette).toHaveBeenCalledTimes(2);
  });

  it("连续输入 g d 时导航到仪表盘", () => {
    const onTogglePalette = vi.fn();
    const onNavigate = vi.fn();
    renderHook(() => useGlobalHotkeys({ onTogglePalette, onNavigate }));

    window.dispatchEvent(new KeyboardEvent("keydown", { key: "g" }));
    window.dispatchEvent(new KeyboardEvent("keydown", { key: "d" }));

    expect(onNavigate).toHaveBeenCalledWith("/");
  });

  it("输入框获得焦点时不触发字母序列快捷键", () => {
    const onTogglePalette = vi.fn();
    const onNavigate = vi.fn();
    renderHook(() => useGlobalHotkeys({ onTogglePalette, onNavigate }));

    const input = document.createElement("input");
    document.body.appendChild(input);
    input.focus();

    input.dispatchEvent(new KeyboardEvent("keydown", { key: "g", bubbles: true }));
    input.dispatchEvent(new KeyboardEvent("keydown", { key: "d", bubbles: true }));

    expect(onNavigate).not.toHaveBeenCalled();
    document.body.removeChild(input);
  });
});

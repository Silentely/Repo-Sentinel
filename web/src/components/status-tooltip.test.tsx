import { act, fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { StatusTooltip } from "./status-tooltip";

describe("StatusTooltip", () => {
  it("默认只渲染子元素，不渲染浮层", () => {
    render(
      <StatusTooltip title="测试标题">
        <span data-testid="badge">测试胶囊</span>
      </StatusTooltip>,
    );

    expect(screen.getByTestId("badge")).toBeInTheDocument();
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();
  });

  it("hover 时在延迟后展示浮层，并正确展示标题、详情行与告警", () => {
    vi.useFakeTimers();

    render(
      <StatusTooltip
        title="GitHub API 状态"
        badge={{ text: "降级", variant: "warning" }}
        rows={[{ label: "剩余配额", value: "0 / 0" }]}
        alert={{ type: "danger", message: "探针超时" }}
        hint="请检查网络配置"
      >
        <span data-testid="badge">GH: 降级</span>
      </StatusTooltip>,
    );

    const trigger = screen.getByTestId("badge");
    fireEvent.mouseEnter(trigger);

    // 还没过 100ms 前不出现
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();

    // 推进 120ms 后展示
    act(() => {
      vi.advanceTimersByTime(120);
    });

    const panel = screen.getByTestId("status-tooltip-panel");
    expect(panel).toBeInTheDocument();
    expect(screen.getByText("GitHub API 状态")).toBeInTheDocument();
    expect(screen.getByText("降级")).toBeInTheDocument();
    expect(screen.getByText("剩余配额")).toBeInTheDocument();
    expect(screen.getByText("0 / 0")).toBeInTheDocument();
    expect(screen.getByText("探针超时")).toBeInTheDocument();
    expect(screen.getByText("请检查网络配置")).toBeInTheDocument();

    // 鼠标移出后 150ms 自动关闭
    fireEvent.mouseLeave(trigger);
    act(() => {
      vi.advanceTimersByTime(160);
    });
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();

    vi.useRealTimers();
  });

  it("点击胶囊可锁定浮层（Pin），点击外部可收起", () => {
    render(
      <div>
        <div data-testid="outside">外部区域</div>
        <StatusTooltip title="DB 状态" badge={{ text: "正常", variant: "success" }}>
          <span data-testid="badge">DB: OK</span>
        </StatusTooltip>
      </div>,
    );

    const trigger = screen.getByTestId("badge");
    fireEvent.click(trigger);

    expect(screen.getByTestId("status-tooltip-panel")).toBeInTheDocument();

    // 点击外部关闭
    fireEvent.mouseDown(screen.getByTestId("outside"));
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();
  });

  it("按下 Escape 键可快速关闭浮层", () => {
    render(
      <StatusTooltip title="DB 状态">
        <span data-testid="badge">DB: OK</span>
      </StatusTooltip>,
    );

    const trigger = screen.getByTestId("badge");
    fireEvent.click(trigger);
    expect(screen.getByTestId("status-tooltip-panel")).toBeInTheDocument();

    fireEvent.keyDown(trigger, { key: "Escape" });
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();
  });

  it("按下 Enter 或 Space 键可锁定/关闭浮层，并同步 aria-expanded 与 aria-controls", () => {
    render(
      <StatusTooltip title="DB 状态">
        <span data-testid="badge">DB: OK</span>
      </StatusTooltip>,
    );

    const trigger = screen.getByTestId("badge");
    const wrapper = trigger.closest(".status-tooltip-wrapper")!;
    expect(wrapper).toHaveAttribute("aria-expanded", "false");
    expect(wrapper).not.toHaveAttribute("aria-controls");

    // Enter 打开并固定
    fireEvent.keyDown(trigger, { key: "Enter" });
    expect(screen.getByTestId("status-tooltip-panel")).toBeInTheDocument();
    expect(wrapper).toHaveAttribute("aria-expanded", "true");
    const panelId = screen.getByTestId("status-tooltip-panel").getAttribute("id");
    expect(panelId).toBeTruthy();
    expect(wrapper).toHaveAttribute("aria-controls", panelId);

    // Space 再次触发可收起
    fireEvent.keyDown(trigger, { key: " " });
    expect(screen.queryByTestId("status-tooltip-panel")).not.toBeInTheDocument();
    expect(wrapper).toHaveAttribute("aria-expanded", "false");
  });

  it("离开触发器后快速点击锁定，不会被 150ms 离开定时器错误关闭", () => {
    vi.useFakeTimers();

    render(
      <StatusTooltip title="DB 状态">
        <span data-testid="badge">DB: OK</span>
      </StatusTooltip>,
    );

    const trigger = screen.getByTestId("badge");
    fireEvent.mouseEnter(trigger);
    act(() => {
      vi.advanceTimersByTime(120);
    });
    expect(screen.getByTestId("status-tooltip-panel")).toBeInTheDocument();

    // 触发鼠标移出，启动 150ms 关闭定时器
    fireEvent.mouseLeave(trigger);

    // 在 50ms 时用户点击进行固定（Pin）
    act(() => {
      vi.advanceTimersByTime(50);
    });
    fireEvent.click(trigger);

    // 继续推进 200ms（越过原来的 150ms 关定时器）
    act(() => {
      vi.advanceTimersByTime(200);
    });

    // 浮层依然保持锁定可见
    expect(screen.getByTestId("status-tooltip-panel")).toBeInTheDocument();

    vi.useRealTimers();
  });
});

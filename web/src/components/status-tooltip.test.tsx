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
});

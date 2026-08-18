import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { starTotalWithDelta, starTrendYDomain, StarTrendChart } from "./star-trend-chart";

const points = [
  { date: "2026-08-01", total: 10 },
  { date: "2026-08-02", total: 15 },
];

describe("starTotalWithDelta", () => {
  it("首日无参照只显示总数", () => {
    expect(starTotalWithDelta(10, null)).toBe("10");
  });

  it("增长/回落都带符号展示", () => {
    expect(starTotalWithDelta(15, 5)).toBe("15（较前日 +5）");
    expect(starTotalWithDelta(12, -3)).toBe("12（较前日 -3）");
  });
});

describe("starTrendYDomain", () => {
  it("大基数小波动：围绕数据外扩，不再从 0 起压平", () => {
    expect(starTrendYDomain([
      { date: "2026-08-01", total: 2995 },
      { date: "2026-08-02", total: 3000 },
    ])).toEqual([2975, 3020]);
  });

  it("波动只有个位数时收紧窗口，保证增长可见", () => {
    expect(starTrendYDomain([
      { date: "2026-08-01", total: 3000 },
      { date: "2026-08-02", total: 3001 },
    ])).toEqual([2980, 3021]);
  });

  it("大跨度历史（全部范围）保持整条曲线可见", () => {
    expect(starTrendYDomain([
      { date: "2025-01-01", total: 0 },
      { date: "2026-08-01", total: 3000 },
    ])).toEqual([0, 3100]);
  });

  it("下限不低于 0", () => {
    expect(starTrendYDomain([
      { date: "2026-08-01", total: 10 },
      { date: "2026-08-02", total: 15 },
    ])).toEqual([0, 35]);
  });

  it("波动超过 50 时贴近上下 100 的常规观感", () => {
    expect(starTrendYDomain([
      { date: "2026-08-01", total: 2900 },
      { date: "2026-08-02", total: 3000 },
    ])).toEqual([2800, 3100]);
  });

  it("空数据返回安全兜底区间", () => {
    expect(starTrendYDomain([])).toEqual([0, 100]);
  });
});

describe("StarTrendChart", () => {
  it("渲染标题、当前值与范围切换按钮", () => {
    render(<StarTrendChart points={points} days={30} onDaysChange={() => {}} loading={false} />);
    // 头部标题与末点累计值。
    expect(screen.getByText("全部仓库 Star 总数")).toBeTruthy();
    expect(screen.getByText("15")).toBeTruthy();
    expect(screen.getByText("7 天")).toBeTruthy();
    expect(screen.getByText("30 天")).toBeTruthy();
    expect(screen.getByText("90 天")).toBeTruthy();
    expect(screen.getByText("全部")).toBeTruthy();
  });

  it("空数据显示空态文案", () => {
    render(<StarTrendChart points={[]} days={30} onDaysChange={() => {}} loading={false} />);
    expect(screen.getByText(/暂无 star 数据/)).toBeTruthy();
  });

  it("加载中显示加载文案，且不出现空态文案", () => {
    render(<StarTrendChart points={[]} days={30} onDaysChange={() => {}} loading />);
    expect(screen.getByText("加载中…")).toBeTruthy();
    expect(screen.queryByText(/暂无 star 数据/)).toBeNull();
  });

  it("点击范围按钮回调对应天数，并标记选中态", async () => {
    const user = userEvent.setup();
    const onDaysChange = vi.fn();
    render(<StarTrendChart points={points} days={7} onDaysChange={onDaysChange} loading={false} />);

    const group = screen.getByRole("group", { name: "时间范围" });
    expect(group.querySelector(".active")?.textContent).toBe("7 天");

    await user.click(screen.getByRole("button", { name: "全部" }));
    expect(onDaysChange).toHaveBeenCalledWith(0);
    await user.click(screen.getByRole("button", { name: "90 天" }));
    expect(onDaysChange).toHaveBeenCalledWith(90);
  });
});

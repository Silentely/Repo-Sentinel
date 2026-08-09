import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { starTotalWithDelta, StarTrendChart } from "./star-trend-chart";

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
    expect(group.querySelector(".is-active")?.textContent).toBe("7 天");

    await user.click(screen.getByRole("button", { name: "全部" }));
    expect(onDaysChange).toHaveBeenCalledWith(0);
    await user.click(screen.getByRole("button", { name: "90 天" }));
    expect(onDaysChange).toHaveBeenCalledWith(90);
  });
});

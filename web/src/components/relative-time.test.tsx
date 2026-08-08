import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { RelativeTime } from "./relative-time";

describe("RelativeTime", () => {
  it("渲染相对时间文案", () => {
    render(<RelativeTime date="2026-08-08T10:00:00Z" />);
    expect(screen.getByText(/前$/)).toBeInTheDocument();
  });

  it("hover 时 title 展示绝对时间", () => {
    render(<RelativeTime date="2026-08-08T10:00:00Z" />);
    const time = screen.getByText(/前$/);
    expect(time.tagName).toBe("TIME");
    expect(time).toHaveAttribute("dateTime", "2026-08-08T10:00:00Z");
    expect(time.getAttribute("title")).toBe(
      new Date("2026-08-08T10:00:00Z").toLocaleString("zh-CN"),
    );
  });

  it("透传 className 供既有样式复用", () => {
    render(<RelativeTime date="2026-08-08T10:00:00Z" className="event-time" />);
    expect(screen.getByText(/前$/)).toHaveClass("event-time");
  });

  it("支持前缀文案（如「最后同步: 」）", () => {
    render(<RelativeTime date="2026-08-08T10:00:00Z" prefix="最后同步: " />);
    expect(screen.getByText(/^最后同步: /)).toBeInTheDocument();
  });

  it("空值与非法日期不渲染节点", () => {
    const { container, rerender } = render(<RelativeTime date="" />);
    expect(container.firstChild).toBeNull();
    rerender(<RelativeTime date="not-a-date" />);
    expect(container.firstChild).toBeNull();
  });
});

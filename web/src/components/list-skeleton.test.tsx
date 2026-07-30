import { render, screen } from "@testing-library/react";
import { describe, expect, it } from "vitest";

import { ListSkeleton } from "./list-skeleton";

describe("ListSkeleton", () => {
  it("渲染默认 5 行骨架并声明加载状态", () => {
    render(<ListSkeleton />);
    expect(screen.getByRole("status", { name: "加载中" })).toBeInTheDocument();
    expect(document.querySelectorAll(".skeleton-row")).toHaveLength(5);
  });

  it("支持自定义行数", () => {
    render(<ListSkeleton rows={3} />);
    expect(document.querySelectorAll(".skeleton-row")).toHaveLength(3);
  });
});

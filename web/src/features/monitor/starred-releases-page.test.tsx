import { describe, expect, it, vi } from "vitest";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { TrackerRow } from "./starred-releases-page";
import type { StarredTrackerItem } from "./api";

const base: StarredTrackerItem = {
  id: "tk-1",
  full_name: "octocat/Hello-World",
  state: "tracking",
  last_release_tag: "v1.0",
  last_release_published_at: "2026-08-01T00:00:00Z",
  last_poll_at: "2026-08-15T00:00:00Z",
  first_seen_at: "2026-08-10T00:00:00Z",
};

function row(overrides: Partial<StarredTrackerItem>) {
  return { ...base, ...overrides };
}

describe("TrackerRow 操作按钮", () => {
  it("tracking 仅显示停用", () => {
    render(<TrackerRow item={row({ state: "tracking" })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: "停用" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "恢复" })).not.toBeInTheDocument();
  });

  it("无 Release 但带已记录 release 时显示恢复与停用", () => {
    render(<TrackerRow item={row({ state: "inactive", last_release_tag: "v1.0" })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: "恢复" })).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "停用" })).toBeInTheDocument();
  });

  it("无 Release 且从未发布 release 时仅显示停用", () => {
    render(<TrackerRow item={row({ state: "inactive", last_release_tag: undefined })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: "停用" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "恢复" })).not.toBeInTheDocument();
  });

  it("不可用仅显示停用", () => {
    render(<TrackerRow item={row({ state: "unavailable" })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: "停用" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "恢复" })).not.toBeInTheDocument();
  });

  it("已停用仅显示恢复", () => {
    render(<TrackerRow item={row({ state: "disabled" })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByRole("button", { name: "恢复" })).toBeInTheDocument();
    expect(screen.queryByRole("button", { name: "停用" })).not.toBeInTheDocument();
  });

  it("恢复与停用分别回调目标状态", async () => {
    const user = userEvent.setup();
    const onToggle = vi.fn();
    render(<TrackerRow item={row({ state: "inactive", last_release_tag: "v1.0" })} busy={false} onToggle={onToggle} />);
    await user.click(screen.getByRole("button", { name: "恢复" }));
    await user.click(screen.getByRole("button", { name: "停用" }));
    expect(onToggle).toHaveBeenNthCalledWith(1, "tracking");
    expect(onToggle).toHaveBeenNthCalledWith(2, "disabled");
  });
});

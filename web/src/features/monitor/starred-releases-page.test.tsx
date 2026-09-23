import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { act, fireEvent, render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";
import { StarredReleasesPage, TrackerRow } from "./starred-releases-page";
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

// star 同步落定时刻的可编程 mock 状态：advanceAfterPolls 控制第几次配置拉取后
// last_star_sync_at 推进（模拟后端异步同步完成）；neverAdvances 模拟超时未完成。
const syncState = vi.hoisted(() => ({
  lastStarSyncAt: "2026-09-20T01:00:00Z",
  advancedAt: "2026-09-20T02:00:00Z",
  advanceAfterPolls: 2,
  neverAdvances: false,
  polls: 0,
}));

const {
  syncStarredReleasesMock,
  listStarredTrackersMock,
  saveStarredReleasesConfigMock,
  setStarredTrackerStateMock,
} = vi.hoisted(() => ({
  syncStarredReleasesMock: vi.fn(async () => ({ started: true })),
  listStarredTrackersMock: vi.fn(async () => ({
    items: [
      {
        id: "tk-1",
        full_name: "octocat/Hello-World",
        state: "tracking" as const,
        last_release_tag: "v1.0",
        last_release_published_at: "2026-08-01T00:00:00Z",
        last_poll_at: "2026-08-15T00:00:00Z",
        first_seen_at: "2026-08-10T00:00:00Z",
      },
    ],
    page: 1,
    per_page: 20,
    total: 1,
  })),
  saveStarredReleasesConfigMock: vi.fn(async (body: Record<string, unknown>) => body as never),
  setStarredTrackerStateMock: vi.fn(async () => ({ ok: true })),
}));

vi.mock("./api", async () => {
  const actual = await vi.importActual<typeof import("./api")>("./api");
  return {
    ...actual,
    starredReleasesConfigQueryOptions: {
      queryKey: ["starred-releases-config"] as const,
      staleTime: 10_000,
      queryFn: async () => {
        syncState.polls += 1;
        const advanced = !syncState.neverAdvances && syncState.polls >= syncState.advanceAfterPolls;
        return {
          username: "octocat",
          star_sync_interval: "6h0m0s",
          release_poll_interval: "10m0s",
          max_trackers: 500,
          notify_prerelease: false,
          enabled: true,
          ai_release_summary_enabled: false,
          last_star_sync_at: advanced ? syncState.advancedAt : syncState.lastStarSyncAt,
          counts: { tracking: 1, inactive: 0, disabled: 0, unavailable: 0 },
        };
      },
    },
    syncStarredReleases: syncStarredReleasesMock,
    listStarredTrackers: listStarredTrackersMock,
    saveStarredReleasesConfig: saveStarredReleasesConfigMock,
    setStarredTrackerState: setStarredTrackerStateMock,
  };
});

function renderPage() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <StarredReleasesPage />
    </QueryClientProvider>,
  );
}

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

  it("非法发布日期安全回退横线且外部链接具有 noopener noreferrer", () => {
    render(<TrackerRow item={row({ state: "tracking", last_release_published_at: "invalid-date-format" })} busy={false} onToggle={vi.fn()} />);
    expect(screen.getByText(/｜ —/)).toBeInTheDocument();
    const links = screen.getAllByRole("link");
    expect(links.length).toBeGreaterThan(0);
    for (const link of links) {
      expect(link).toHaveAttribute("rel", "noopener noreferrer");
    }
  });
});

describe("StarredReleasesPage 立即同步", () => {
  beforeEach(() => {
    vi.useFakeTimers();
    syncState.polls = 0;
    syncState.advanceAfterPolls = 2;
    syncState.neverAdvances = false;
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("等待同步落定后才刷新追踪列表", async () => {
    renderPage();
    // 初始查询在微任务中落地：推进 0 并包裹 act 让 React 提交渲染。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(listStarredTrackersMock).toHaveBeenCalledTimes(1);
    // POST 返回仅代表同步启动：此刻追踪列表不得提前刷新（否则看到同步前数据）。
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "立即同步 Star 列表" }));
    });
    expect(syncStarredReleasesMock).toHaveBeenCalledTimes(1);
    expect(listStarredTrackersMock).toHaveBeenCalledTimes(1);
    // last_star_sync_at 推进后（异步同步完成）才刷新列表并提示成功。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2100);
    });
    expect(screen.getByText("Star 列表已同步，追踪列表已更新。")).toBeInTheDocument();
    expect(listStarredTrackersMock.mock.calls.length).toBeGreaterThan(1);
  });

  it("超过等待窗口仍提示未完成", async () => {
    syncState.neverAdvances = true;
    renderPage();
    // 初始查询在微任务中落地：推进 0 并包裹 act 让 React 提交渲染。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "立即同步 Star 列表" }));
    });
    // 同步始终未落定：推进等待窗口（90s）后提示稍后查看。
    // 推进量止于窗口结束附近：超过后 useAutoDismiss 的 3s 自动清除定时器会抹掉提示。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(92_000);
    });
    expect(screen.getByText("同步尚未完成，追踪列表请稍后刷新查看。")).toBeInTheDocument();
  });

  it("卸载后立即停止同步轮询", async () => {
    syncState.neverAdvances = true;
    const view = renderPage();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    await act(async () => {
      fireEvent.click(screen.getByRole("button", { name: "立即同步 Star 列表" }));
    });
    // 进入轮询：等第一个轮询周期落地后卸载，此后不得再发起配置拉取。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(2_100);
    });
    const pollsAtUnmount = syncState.polls;
    view.unmount();
    await act(async () => {
      await vi.advanceTimersByTimeAsync(10_000);
    });
    expect(syncState.polls).toBe(pollsAtUnmount);
  });
});

describe("StarredReleasesPage 表单回填", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });

  afterEach(() => {
    vi.useRealTimers();
  });

  it("配置 refetch 不覆盖未保存编辑", async () => {
    renderPage();
    // 初始查询在微任务中落地：推进 0 并包裹 act 让 React 提交渲染。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(0);
    });
    const username = screen.getByPlaceholderText(/可粘贴 github.com/);
    fireEvent.change(username, { target: { value: "octocat-editing" } });
    expect(username).toHaveValue("octocat-editing");
    // 页面重新可见触发配置 refetch（新对象引用）：旧实现会整体回填表单，
    // 把未保存的编辑覆盖为服务端值。先推进超过 staleTime 使查询过期，可见性变化才会真正重拉。
    await act(async () => {
      await vi.advanceTimersByTimeAsync(15_000);
    });
    Object.defineProperty(document, "visibilityState", { value: "visible", configurable: true });
    await act(async () => {
      window.dispatchEvent(new Event("visibilitychange"));
      await vi.advanceTimersByTimeAsync(0);
    });
    expect(screen.getByPlaceholderText(/可粘贴 github.com/)).toHaveValue("octocat-editing");
  });
});

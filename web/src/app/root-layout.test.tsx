import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import type { AuthenticationResponse } from "../features/auth/api";

// 抽屉交互与真实路由解耦：链接渲染为普通锚点，路径固定为 "/"。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => (
    <a
      href={to}
      onClick={(event) => {
        // jsdom 不实现页面跳转，静默默认行为避免控制台噪音。
        event.preventDefault();
      }}
    >
      {children}
    </a>
  ),
  Outlet: () => null,
  useNavigate: () => vi.fn(async () => undefined),
  useRouterState: ({
    select,
  }: {
    select: (state: { location: { pathname: string } }) => string;
  }) => select({ location: { pathname: "/" } }),
}));

// 查询保持 pending：测试聚焦抽屉行为，避免异步回填干扰断言。
// vi.mock 工厂会被提升，queryFn 必须内联，不能引用模块级变量。
vi.mock("../features/auth/api", () => ({
  logout: vi.fn(async () => undefined),
  readyStatusQueryOptions: {
    queryKey: ["test", "ready"],
    queryFn: () => new Promise<never>(() => undefined),
  },
}));

vi.mock("../features/monitor/api", () => ({
  dashboardQueryOptions: {
    queryKey: ["test", "dashboard"],
    queryFn: () => new Promise<never>(() => undefined),
  },
  settingsQueryOptions: {
    queryKey: ["test", "settings"],
    queryFn: () => new Promise<never>(() => undefined),
  },
  versionQueryOptions: {
    queryKey: ["test", "version"],
    queryFn: () => new Promise<never>(() => undefined),
  },
}));

import { RootLayout, mobileTitleFor } from "./root-layout";

const session: AuthenticationResponse = {
  admin: { id: "admin-1", username: "admin" },
  session: { id: "session-1", expires_at: "2099-01-01T00:00:00Z" },
};

function renderLayout() {
  const queryClient = new QueryClient();
  return render(
    <QueryClientProvider client={queryClient}>
      <RootLayout session={session} />
    </QueryClientProvider>,
  );
}

describe("移动端抽屉导航", () => {
  it("默认收起，点击菜单按钮展开抽屉并锁定背景滚动", async () => {
    const user = userEvent.setup();
    renderLayout();

    const menu = screen.getByRole("button", { name: "打开导航菜单" });
    const sidebar = document.getElementById("app-sidebar");
    expect(menu).toHaveAttribute("aria-expanded", "false");
    expect(menu).toHaveAttribute("aria-controls", "app-sidebar");
    expect(sidebar?.className).not.toContain("is-open");

    await user.click(menu);

    expect(menu).toHaveAttribute("aria-expanded", "true");
    expect(sidebar?.className).toContain("is-open");
    expect(document.body.style.overflow).toBe("hidden");
    expect(screen.getByRole("button", { name: "收起导航菜单" })).toHaveFocus();
  });

  it("抽屉内展示完整导航入口", () => {
    renderLayout();
    for (const name of [
      "仪表盘",
      "仓库管理",
      "Issues",
      "Pull Requests",
      "Actions",
      "安全告警",
      "渠道配置",
      "投递记录",
      "GitHub App",
      "关于",
      "设置",
    ]) {
      expect(screen.getByRole("link", { name })).toBeInTheDocument();
    }
  });

  it("点击导航链接后收起抽屉、解锁滚动，焦点回到菜单按钮", async () => {
    const user = userEvent.setup();
    renderLayout();
    const menu = screen.getByRole("button", { name: "打开导航菜单" });
    await user.click(menu);

    await user.click(screen.getByRole("link", { name: "仓库管理" }));

    const sidebar = document.getElementById("app-sidebar");
    expect(sidebar?.className).not.toContain("is-open");
    expect(document.body.style.overflow).toBe("");
    expect(menu).toHaveFocus();
    expect(menu).toHaveAttribute("aria-expanded", "false");
  });

  it("Escape 关闭抽屉", async () => {
    const user = userEvent.setup();
    renderLayout();
    await user.click(screen.getByRole("button", { name: "打开导航菜单" }));
    expect(document.getElementById("app-sidebar")?.className).toContain("is-open");

    await user.keyboard("{Escape}");

    expect(document.getElementById("app-sidebar")?.className).not.toContain("is-open");
    expect(document.body.style.overflow).toBe("");
  });

  it("点击遮罩关闭抽屉", async () => {
    const user = userEvent.setup();
    const { container } = renderLayout();
    await user.click(screen.getByRole("button", { name: "打开导航菜单" }));

    const scrim = container.querySelector(".app-scrim");
    expect(scrim).not.toBeNull();
    await user.click(scrim as HTMLElement);

    expect(document.getElementById("app-sidebar")?.className).not.toContain("is-open");
  });

  it("点击抽屉内关闭按钮收起抽屉", async () => {
    const user = userEvent.setup();
    renderLayout();
    await user.click(screen.getByRole("button", { name: "打开导航菜单" }));

    await user.click(screen.getByRole("button", { name: "收起导航菜单" }));

    expect(document.getElementById("app-sidebar")?.className).not.toContain("is-open");
    expect(screen.getByRole("button", { name: "打开导航菜单" })).toHaveFocus();
  });
});

describe("移动端顶栏标题 mobileTitleFor", () => {
  it("路由前缀映射到中文标题，且与侧栏导航文案一致", () => {
    const cases: [string, string][] = [
      ["/", "仪表盘"],
      ["/repos", "仓库管理"],
      ["/issues", "Issues"],
      ["/pull-requests", "Pull Requests"],
      ["/actions", "Actions"],
      ["/security", "安全告警"],
      ["/notifications", "渠道配置"],
      ["/notifications/outbox", "投递记录"],
      ["/github", "GitHub App"],
      ["/about", "关于"],
      ["/settings", "设置"],
    ];
    for (const [path, expected] of cases) {
      expect(mobileTitleFor(path)).toBe(expected);
    }
  });

  it("子路由继承父级标题：渠道配置子页不被误判为投递记录以外", () => {
    // outbox 必须优先于 /notifications 前缀判定（顺序敏感回归）。
    expect(mobileTitleFor("/notifications/outbox")).toBe("投递记录");
    expect(mobileTitleFor("/notifications/outbox?status=dead")).toBe("投递记录");
    expect(mobileTitleFor("/notifications")).toBe("渠道配置");
    expect(mobileTitleFor("/repos/archive")).toBe("仓库管理");
  });

  it("未知路径回退「仪表盘」", () => {
    expect(mobileTitleFor("/no-such-page")).toBe("仪表盘");
  });
});

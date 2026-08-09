import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { fireEvent, render, screen, within } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

// 页面内链接与真实路由解耦：渲染为普通锚点即可。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

// 版本快照：覆盖「关于」页构建与运行区块的全部字段。
const versionFixture = {
  version: "0.3.8",
  build_channel: "dev",
  git_sha: "abc1234",
  git_branch: "dev",
  build_time: "2026-08-08T04:48:10Z",
  go_version: "go1.26",
  database_driver: "sqlite",
  schema_version: "20260728000100",
  http_addr: "0.0.0.0:8080",
  public_base_url: "https://example.com",
  update_check_enabled: true,
  github: { webhook_path: "/webhooks/github" },
};

const { checkForUpdatesMock } = vi.hoisted(() => ({
  checkForUpdatesMock: vi.fn(async () => ({
    version: versionFixture,
    update_check: {
      enabled: true,
      update_available: false,
      latest_version: "0.3.8",
      latest_url: "https://github.com/Silentely/Repo-Sentinel/releases",
      cached: false,
      error: "",
    },
  })),
}));

// 供单个用例模拟版本查询失败，验证错误提示展示。
let versionQueryError: Error | null = null;

vi.mock("./api", () => ({
  versionQueryOptions: {
    queryKey: ["test", "version"],
    queryFn: async () => {
      if (versionQueryError) {
        throw versionQueryError;
      }
      return versionFixture;
    },
  },
  checkForUpdates: checkForUpdatesMock,
}));

import { AboutPage } from "./about-page";

function renderPage() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <AboutPage />
    </QueryClientProvider>,
  );
}

describe("关于页", () => {
  it("Git SHA 可一键复制，复制后反馈状态", async () => {
    const writeText = vi.fn(async () => undefined);
    // jsdom 的 navigator.clipboard 会随 user-event 交互被重置：用 defineProperty 注入
    // 一次性 mock，并以 fireEvent 触发（fireEvent 不触碰 navigator）。
    Object.defineProperty(navigator, "clipboard", {
      value: { writeText },
      configurable: true,
    });
    renderPage();

    const build = screen.getByRole("region", { name: "构建与运行" });
    await within(build).findByText("abc1234");
    fireEvent.click(within(build).getByRole("button", { name: "复制 Git SHA" }));

    expect(writeText).toHaveBeenCalledWith("abc1234");
    expect(await within(build).findByText("已复制")).toBeInTheDocument();
  });

  it("展示产品介绍、构建信息与运维提示区块", async () => {
    renderPage();

    // 产品区块与文档链接。
    const product = await screen.findByRole("region", { name: "你在用什么" });
    expect(within(product).getByText("配置参考")).toBeInTheDocument();
    expect(within(product).getByText("常见问题")).toBeInTheDocument();
    expect(within(product).getByRole("link", { name: "打开设置" })).toHaveAttribute("href", "/settings");

    // 构建与运行区块：版本字段落定展示。
    const build = screen.getByRole("region", { name: "构建与运行" });
    expect(await within(build).findByText("0.3.8")).toBeInTheDocument();
    expect(within(build).getByText("abc1234")).toBeInTheDocument();
    expect(within(build).getByText("sqlite")).toBeInTheDocument();

    // 运维提示区块。
    expect(screen.getByRole("region", { name: "运维提示" })).toBeInTheDocument();
  });

  it("检查更新后展示已是最新提示", async () => {
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole("region", { name: "构建与运行" });
    await user.click(screen.getByRole("button", { name: "检查更新" }));

    expect(await screen.findByText(/已是最新（远程 v0\.3\.8/)).toBeInTheDocument();
    expect(checkForUpdatesMock).toHaveBeenCalledWith(true);
  });

  it("发现新版本时提示并提供 Release 链接", async () => {
    checkForUpdatesMock.mockResolvedValueOnce({
      version: versionFixture,
      update_check: {
        enabled: true,
        update_available: true,
        latest_version: "9.9.9",
        latest_url: "https://github.com/Silentely/Repo-Sentinel/releases/tag/v9.9.9",
        cached: false,
        error: "",
      },
    });
    const user = userEvent.setup();
    renderPage();

    await screen.findByRole("region", { name: "构建与运行" });
    await user.click(screen.getByRole("button", { name: "检查更新" }));

    expect(await screen.findByText(/发现新版本 v9\.9\.9/)).toBeInTheDocument();
    const releaseLink = screen.getByRole("link", { name: "打开 Release 页面" });
    expect(releaseLink).toHaveAttribute("href", "https://github.com/Silentely/Repo-Sentinel/releases/tag/v9.9.9");
  });

  it("版本查询失败时展示错误提示", async () => {
    versionQueryError = new Error("boom");
    try {
      renderPage();

      const build = await screen.findByRole("region", { name: "构建与运行" });
      expect(await within(build).findByText("无法加载版本")).toBeInTheDocument();
    } finally {
      versionQueryError = null;
    }
  });
});

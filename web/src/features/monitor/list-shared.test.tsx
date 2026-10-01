import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// 与 root-layout.test 保持一致：Link 渲染为普通锚点，避免真实路由依赖。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

import { BatchActionBar, ClearFiltersButton, FeatureGuard, isBotUser, BotBadge } from "./list-shared";

function renderGuard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <FeatureGuard featureKey="feature.issues" featureName="Issues">
        <p>守卫内的页面内容</p>
      </FeatureGuard>
    </QueryClientProvider>,
  );
}

afterEach(() => {
  vi.unstubAllGlobals();
});

describe("FeatureGuard 功能开关守卫", () => {
  it("开关查询失败时展示错误态而非静默放行", async () => {
    // settings 接口返回 500：守卫必须显式报错，不能按「启用」放行页面内容。
    vi.stubGlobal(
      "fetch",
      vi.fn(async () => new Response(JSON.stringify({ error_code: "internal_error", message: "服务器错误" }), { status: 500 })),
    );

    renderGuard();

    expect(await screen.findByText("无法加载功能开关")).toBeInTheDocument();
    expect(screen.getByText("服务器错误")).toBeInTheDocument();
    expect(screen.queryByText("守卫内的页面内容")).not.toBeInTheDocument();
  });
});

describe("ClearFiltersButton", () => {
  it("渲染无障碍 aria-label 属性", () => {
    const fn = vi.fn();
    render(<ClearFiltersButton onClick={fn} />);
    const btn = screen.getByRole("button", { name: "清除当前所有筛选条件" });
    expect(btn).toBeInTheDocument();
  });
});

describe("BatchActionBar", () => {
  it("selectedCount 为 0 时不渲染", () => {
    const { container } = render(
      <BatchActionBar
        selectedCount={0}
        onClear={vi.fn()}
        onBatchIgnore={vi.fn()}
      />
    );
    expect(container.firstChild).toBeNull();
  });

  it("渲染已选数量并响应点击", () => {
    const onClear = vi.fn();
    const onBatchIgnore = vi.fn();
    render(
      <BatchActionBar
        selectedCount={3}
        onClear={onClear}
        onBatchIgnore={onBatchIgnore}
      />
    );

    expect(screen.getByText("已选择 3 项")).toBeInTheDocument();
    const ignoreBtn = screen.getByRole("button", { name: "批量忽略" });
    const clearBtn = screen.getByRole("button", { name: "取消选择" });

    ignoreBtn.click();
    expect(onBatchIgnore).toHaveBeenCalledTimes(1);

    clearBtn.click();
    expect(onClear).toHaveBeenCalledTimes(1);
  });

  it("isPending 为 true 时禁用按钮并显示加载文案", () => {
    render(
      <BatchActionBar
        selectedCount={2}
        onClear={vi.fn()}
        onBatchIgnore={vi.fn()}
        isPending={true}
      />
    );
    const ignoreBtn = screen.getByRole("button", { name: "处理中…" });
    expect(ignoreBtn).toBeDisabled();
  });
});

describe("isBotUser 识别规则", () => {
  it("识别以 [bot] 为后缀的机器账号", () => {
    expect(isBotUser("renovate[bot]")).toBe(true);
    expect(isBotUser("dependabot[bot]")).toBe(true);
    expect(isBotUser("my-custom-bot[bot]")).toBe(true);
  });

  it("识别白名单内的常用服务机器账号", () => {
    expect(isBotUser("dependabot")).toBe(true);
    expect(isBotUser("renovate")).toBe(true);
    expect(isBotUser("github-actions")).toBe(true);
    expect(isBotUser("snyk-bot")).toBe(true);
    expect(isBotUser("codecov")).toBe(true);
    expect(isBotUser("copilot")).toBe(true);
  });

  it("仅识别后端 botutil 同名规则，未知前缀账号按真人处理", () => {
    // 后端 botutil 不匹配 bot- 前缀与未列入名单的服务名，前端兜底必须同口径。
    expect(isBotUser("bot-worker")).toBe(false);
    expect(isBotUser("sonarcloud")).toBe(false);
    expect(isBotUser("stale")).toBe(false);
  });

  it("正常人类开发者账号返回 false", () => {
    expect(isBotUser("octocat")).toBe(false);
    expect(isBotUser("alice")).toBe(false);
    expect(isBotUser(null)).toBe(false);
    expect(isBotUser(undefined)).toBe(false);
    expect(isBotUser("")).toBe(false);
  });
});

describe("BotBadge 组件", () => {
  it("渲染带有 BOT 文本的徽章", () => {
    render(<BotBadge />);
    expect(screen.getByText("BOT")).toBeInTheDocument();
  });
});

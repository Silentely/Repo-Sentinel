import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// 与 root-layout.test 保持一致：Link 渲染为普通锚点，避免真实路由依赖。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

import { BatchActionBar, ClearFiltersButton, FeatureGuard } from "./list-shared";

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

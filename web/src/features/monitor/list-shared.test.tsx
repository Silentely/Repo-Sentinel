import type { ReactNode } from "react";
import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import { afterEach, describe, expect, it, vi } from "vitest";

// 与 root-layout.test 保持一致：Link 渲染为普通锚点，避免真实路由依赖。
vi.mock("@tanstack/react-router", () => ({
  Link: ({ to, children }: { to: string; children?: ReactNode }) => <a href={to}>{children}</a>,
}));

import { FeatureGuard } from "./list-shared";

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

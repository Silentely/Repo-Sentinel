import { fireEvent, render, screen } from "@testing-library/react";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../lib/api/errors";

import { QueryGate } from "./query-gate";

describe("QueryGate", () => {
  it("查询失败时渲染错误提示，不渲染空态与内容", () => {
    const error = new ApiError({ status: 500, errorCode: "boom", message: "后端暂时不可用" });
    render(
      <QueryGate query={{ isPending: false, isError: true, error }} isEmpty emptyState={<p>空空如也</p>}>
        <p>列表内容</p>
      </QueryGate>,
    );

    const alert = screen.getByRole("alert");
    expect(alert).toHaveTextContent("加载失败");
    expect(alert).toHaveTextContent("后端暂时不可用");
    expect(alert).toHaveTextContent("boom");
    expect(screen.queryByText("空空如也")).not.toBeInTheDocument();
    expect(screen.queryByText("列表内容")).not.toBeInTheDocument();
  });

  it("查询失败且提供 refetch 时显示重试按钮", () => {
    const refetch = vi.fn();
    render(
      <QueryGate
        query={{ isPending: false, isError: true, error: new Error("网络抖动"), refetch }}
        isEmpty={false}
        emptyState={<p>空空如也</p>}
      >
        <p>列表内容</p>
      </QueryGate>,
    );

    const retryButton = screen.getByRole("button", { name: "重试" });
    fireEvent.click(retryButton);
    expect(refetch).toHaveBeenCalledTimes(1);
  });

  it("加载中渲染默认骨架屏，支持 isPending 与旧命名 isLoading", () => {
    const { rerender } = render(
      <QueryGate query={{ isPending: true, isError: false }} isEmpty={false} emptyState={<p>空空如也</p>}>
        <p>列表内容</p>
      </QueryGate>,
    );
    expect(screen.getByRole("status", { name: "加载中" })).toBeInTheDocument();

    // 兼容仅提供 isLoading 的调用方（TanStack Query v4 命名）。
    rerender(
      <QueryGate query={{ isLoading: true, isError: false }} isEmpty={false} emptyState={<p>空空如也</p>}>
        <p>列表内容</p>
      </QueryGate>,
    );
    expect(screen.getByRole("status", { name: "加载中" })).toBeInTheDocument();
  });

  it("支持自定义加载占位", () => {
    render(
      <QueryGate
        query={{ isPending: true, isError: false }}
        skeleton={<p>定制加载中</p>}
        isEmpty={false}
        emptyState={<p>空空如也</p>}
      >
        <p>列表内容</p>
      </QueryGate>,
    );
    expect(screen.getByText("定制加载中")).toBeInTheDocument();
    expect(screen.queryByRole("status", { name: "加载中" })).not.toBeInTheDocument();
  });

  it("加载完成且为空时渲染空态", () => {
    render(
      <QueryGate query={{ isPending: false, isError: false }} isEmpty emptyState={<p>空空如也</p>}>
        <p>列表内容</p>
      </QueryGate>,
    );
    expect(screen.getByText("空空如也")).toBeInTheDocument();
    expect(screen.queryByText("列表内容")).not.toBeInTheDocument();
  });

  it("加载完成且有数据时渲染子内容", () => {
    render(
      <QueryGate query={{ isPending: false, isError: false }} isEmpty={false} emptyState={<p>空空如也</p>}>
        <p>列表内容</p>
      </QueryGate>,
    );
    expect(screen.getByText("列表内容")).toBeInTheDocument();
    expect(screen.queryByText("空空如也")).not.toBeInTheDocument();
  });
});

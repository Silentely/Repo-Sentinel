import { describe, expect, it } from "vitest";
import { render, screen } from "@testing-library/react";

import { EmptyState } from "./empty-state";
import { ErrorAlert, ApiErrorAlert } from "./error-alert";
import { ApiError } from "../lib/api/errors";

describe("EmptyState 组件", () => {
  it("渲染标题、描述与无障碍标签", () => {
    render(<EmptyState title="暂无数据" description="请稍后重试" />);
    expect(screen.getByRole("region", { name: "暂无数据" })).toBeInTheDocument();
    expect(screen.getByText("暂无数据")).toBeInTheDocument();
    expect(screen.getByText("请稍后重试")).toBeInTheDocument();
  });

  it("空描述不渲染多余段落", () => {
    const { container } = render(<EmptyState title="空状态标题" description="" />);
    expect(container.querySelectorAll("p").length).toBe(0);
  });
});

describe("ErrorAlert 组件", () => {
  it("空标题安全回退默认文案", () => {
    render(<ErrorAlert title="" message="连接超时" errorCode="timeout" />);
    expect(screen.getByText("发生错误")).toBeInTheDocument();
    expect(screen.getByText("连接超时")).toBeInTheDocument();
    expect(screen.getByText("timeout")).toBeInTheDocument();
  });

  it("ApiErrorAlert 正常透传错误", () => {
    render(<ApiErrorAlert title="请求失败" error={new ApiError({ status: 500, errorCode: "server_error", message: "服务器异常" })} />);
    expect(screen.getByText("请求失败")).toBeInTheDocument();
    expect(screen.getByText("服务器异常")).toBeInTheDocument();
    expect(screen.getByText("server_error")).toBeInTheDocument();
  });
});

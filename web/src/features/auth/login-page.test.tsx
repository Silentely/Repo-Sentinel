import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../../lib/api/errors";
import { LoginPage } from "./login-page";

describe("登录页", () => {
  it("设置标签页标题", () => {
    render(<LoginPage loginAction={vi.fn()} />);
    expect(document.title).toBe("登录 · RepoSentinel");
  });

  it("右上角提供 GitHub 仓库入口", () => {
    render(<LoginPage loginAction={vi.fn()} />);
    const link = screen.getByRole("link", { name: "GitHub 仓库" });
    expect(link).toHaveAttribute("href", "https://github.com/Silentely/Repo-Sentinel");
    expect(link).toHaveAttribute("target", "_blank");
    expect(link).toHaveAttribute("rel", "noreferrer");
  });

  it("页面载入后用户名输入框自动聚焦", () => {
    render(<LoginPage loginAction={vi.fn()} />);
    expect(screen.getByLabelText("用户名")).toHaveFocus();
  });

  it("展示准确产品信息、认证字段与 CLI 恢复入口", () => {
    render(<LoginPage loginAction={vi.fn()} version="dev" />);

    expect(screen.getByRole("heading", { name: "RepoSentinel" })).toBeInTheDocument();
    expect(screen.getByText("自托管的 GitHub 仓库监控")).toBeInTheDocument();
    expect(screen.getByLabelText("用户名")).toBeInTheDocument();
    expect(screen.getByLabelText("密码")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "登录" })).toBeInTheDocument();
    const recoveryLink = screen.getByRole("link", { name: "使用 CLI 重置密码" });
    expect(recoveryLink).toHaveAttribute(
      "href",
      "https://github.com/Silentely/Repo-Sentinel/blob/main/docs/operations/administrator-access.md#使用-cli-重置密码",
    );
    expect(recoveryLink).toHaveAttribute("target", "_blank");
    expect(recoveryLink).toHaveAttribute("rel", "noreferrer");
    expect(screen.queryByText(/忘记密码/)).not.toBeInTheDocument();
  });

  it("就地解释 invalid_credentials 并给出下一步", async () => {
    const user = userEvent.setup();
    const loginAction = vi.fn(async () => {
      throw new ApiError({
        status: 401,
        errorCode: "invalid_credentials",
        message: "凭据无效。",
      });
    });
    render(<LoginPage loginAction={loginAction} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "错误密码一二三四五六");
    await user.click(screen.getByRole("button", { name: "登录" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("用户名或密码不正确");
    expect(alert).toHaveTextContent("使用 CLI 重置密码");
    expect(alert).toHaveTextContent("invalid_credentials");
  });

  it("凭据失败后清空密码并聚焦密码框，避免旧输入残留", async () => {
    const user = userEvent.setup();
    const loginAction = vi.fn(async () => {
      throw new ApiError({
        status: 401,
        errorCode: "invalid_credentials",
        message: "凭据无效。",
      });
    });
    render(<LoginPage loginAction={loginAction} />);

    const password = screen.getByLabelText("密码");
    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(password, "错误密码一二三四五六");
    await user.click(screen.getByRole("button", { name: "登录" }));

    await screen.findByRole("alert");
    expect(password).toHaveValue("");
    expect(password).toHaveFocus();
  });

  it("rate_limited 时给出限流说明与等待提示", async () => {
    const user = userEvent.setup();
    const loginAction = vi.fn(async () => {
      throw new ApiError({
        status: 429,
        errorCode: "rate_limited",
        message: "登录尝试过于频繁，请稍后再试。",
      });
    });
    render(<LoginPage loginAction={loginAction} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "管理员密码一二三四五六");
    await user.click(screen.getByRole("button", { name: "登录" }));

    const alert = await screen.findByRole("alert");
    expect(alert).toHaveTextContent("尝试过于频繁");
    expect(alert).toHaveTextContent("稍候片刻再试");
    expect(alert).toHaveTextContent("rate_limited");
  });

  it("提交期间禁用按钮并阻止重复登录", async () => {
    const user = userEvent.setup();
    let resolveLogin: (() => void) | undefined;
    const loginAction = vi.fn(
      () =>
        new Promise<void>((resolve) => {
          resolveLogin = resolve;
        }),
    );
    render(<LoginPage loginAction={loginAction} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "管理员密码一二三四五六");
    const submit = screen.getByRole("button", { name: "登录" });
    await user.click(submit);
    await user.click(submit);

    expect(loginAction).toHaveBeenCalledTimes(1);
    expect(submit).toBeDisabled();
    resolveLogin?.();
  });
});

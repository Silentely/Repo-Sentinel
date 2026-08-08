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

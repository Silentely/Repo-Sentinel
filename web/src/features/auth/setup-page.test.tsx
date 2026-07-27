import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { SetupPage } from "./setup-page";

describe("首次设置页", () => {
  it("按 Unicode 字符数拒绝不足 12 字符的密码", async () => {
    const user = userEvent.setup();
    const setupAction = vi.fn(async () => undefined);
    render(<SetupPage setupAction={setupAction} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "😀😀😀😀😀😀");
    await user.type(screen.getByLabelText("确认密码"), "😀😀😀😀😀😀");
    await user.click(screen.getByRole("button", { name: "创建管理员" }));

    expect(await screen.findByText("密码至少需要 12 个 Unicode 字符。")).toBeInTheDocument();
    expect(setupAction).not.toHaveBeenCalled();
  });

  it("密码确认不一致时就地阻止创建", async () => {
    const user = userEvent.setup();
    const setupAction = vi.fn(async () => undefined);
    render(<SetupPage setupAction={setupAction} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "管理员初始密码一二三四五六");
    await user.type(screen.getByLabelText("确认密码"), "另一组管理员密码一二三四五六");
    await user.click(screen.getByRole("button", { name: "创建管理员" }));

    expect(await screen.findByText("两次输入的密码不一致。")).toBeInTheDocument();
    expect(setupAction).not.toHaveBeenCalled();
  });

  it("创建成功后进入根路由", async () => {
    const user = userEvent.setup();
    const setupAction = vi.fn(async () => undefined);
    const onCreated = vi.fn();
    render(<SetupPage setupAction={setupAction} onCreated={onCreated} />);

    await user.type(screen.getByLabelText("用户名"), "Repo Admin");
    await user.type(screen.getByLabelText("密码"), "管理员初始密码一二三四五六");
    await user.type(screen.getByLabelText("确认密码"), "管理员初始密码一二三四五六");
    await user.click(screen.getByRole("button", { name: "创建管理员" }));

    expect(setupAction).toHaveBeenCalledWith({
      username: "Repo Admin",
      password: "管理员初始密码一二三四五六",
    });
    expect(onCreated).toHaveBeenCalledWith("/");
  });
});

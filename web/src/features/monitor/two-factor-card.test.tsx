import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen, waitFor } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { beforeEach, describe, expect, it, vi } from "vitest";

import { TwoFactorCard } from "./two-factor-card";

const { get2FAStatusMock, setup2FAMock, enable2FAMock, disable2FAMock } = vi.hoisted(() => ({
  get2FAStatusMock: vi.fn(),
  setup2FAMock: vi.fn(),
  enable2FAMock: vi.fn(),
  disable2FAMock: vi.fn(),
}));

vi.mock("../auth/api", () => ({
  get2FAStatus: get2FAStatusMock,
  setup2FA: setup2FAMock,
  enable2FA: enable2FAMock,
  disable2FA: disable2FAMock,
}));

function renderCard() {
  const queryClient = new QueryClient({
    defaultOptions: { queries: { retry: false } },
  });
  return render(
    <QueryClientProvider client={queryClient}>
      <TwoFactorCard />
    </QueryClientProvider>,
  );
}

describe("TwoFactorCard", () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it("当 2FA 未开启时展示未开启徽标与配置向导按钮", async () => {
    get2FAStatusMock.mockResolvedValueOnce({ enabled: false });
    renderCard();

    expect(await screen.findByText("两步验证 (2FA / TOTP)")).toBeInTheDocument();
    expect(screen.getByText("未开启")).toBeInTheDocument();
    expect(screen.getByRole("button", { name: "配置并开启两步验证" })).toBeInTheDocument();
  });

  it("点击配置进入绑定流程，展示密钥并支持输入动态验证码确认开启", async () => {
    const user = userEvent.setup();
    get2FAStatusMock.mockResolvedValueOnce({ enabled: false });
    setup2FAMock.mockResolvedValueOnce({
      secret: "JBSWY3DPEHPK3PXP",
      otpauth_url: "otpauth://totp/RepoSentinel:admin?secret=JBSWY3DPEHPK3PXP",
    });
    enable2FAMock.mockResolvedValueOnce({ enabled: true });

    renderCard();

    const setupBtn = await screen.findByRole("button", { name: "配置并开启两步验证" });
    await user.click(setupBtn);

    expect(setup2FAMock).toHaveBeenCalledTimes(1);
    expect(await screen.findByText("第 1 步：在验证器中绑定密钥")).toBeInTheDocument();
    expect(screen.getByText("JBSWY3DPEHPK3PXP")).toBeInTheDocument();

    const input = screen.getByPlaceholderText("000000");
    await user.type(input, "654321");

    const enableBtn = screen.getByRole("button", { name: "确认并开启" });
    await user.click(enableBtn);

    expect(enable2FAMock).toHaveBeenCalledWith({
      secret: "JBSWY3DPEHPK3PXP",
      passcode: "654321",
    });
    expect(await screen.findByText("二步验证已成功开启！下次登录时将需要动态验证码。")).toBeInTheDocument();
  });

  it("当 2FA 已开启时展示已开启徽标与关闭入口，输入密码确认关闭", async () => {
    const user = userEvent.setup();
    get2FAStatusMock.mockResolvedValueOnce({ enabled: true });
    disable2FAMock.mockResolvedValueOnce({ enabled: false });

    renderCard();

    expect(await screen.findByText("已开启")).toBeInTheDocument();
    const closeBtn = screen.getByRole("button", { name: "关闭两步验证" });
    await user.click(closeBtn);

    expect(screen.getByText("关闭两步验证需核验当前管理员密码：")).toBeInTheDocument();
    const pwInput = screen.getByPlaceholderText("请输入当前管理员密码");
    await user.type(pwInput, "admin-secret-pass");

    const confirmCloseBtn = screen.getByRole("button", { name: "确认关闭" });
    await user.click(confirmCloseBtn);

    expect(disable2FAMock).toHaveBeenCalledWith({
      current_password: "admin-secret-pass",
    });
    expect(await screen.findByText("二步验证已关闭。")).toBeInTheDocument();
  });
});

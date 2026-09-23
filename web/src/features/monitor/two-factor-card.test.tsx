import { QueryClient, QueryClientProvider } from "@tanstack/react-query";
import { render, screen } from "@testing-library/react";
import userEvent from "@testing-library/user-event";
import { describe, expect, it, vi } from "vitest";

import { ApiError } from "../../lib/api/errors";
import { TwoFactorCard } from "./two-factor-card";
import type { TwoFactorSetup } from "../auth/api";

const { statusMock, setupMock, enableMock } = vi.hoisted(() => ({
  statusMock: vi.fn(),
  setupMock: vi.fn(),
  enableMock: vi.fn(),
}));

vi.mock("../auth/api", async () => {
  const actual = await vi.importActual<typeof import("../auth/api")>("../auth/api");
  return {
    ...actual,
    get2FAStatus: statusMock,
    setup2FA: setupMock,
    enable2FA: enableMock,
    disable2FA: vi.fn(),
  };
});

const setup: TwoFactorSetup = { secret: "JBSWY3DPEHPK3PXP", otpauth_url: "otpauth://totp/x" };

function renderCard() {
  const queryClient = new QueryClient({ defaultOptions: { queries: { retry: false } } });
  return render(
    <QueryClientProvider client={queryClient}>
      <TwoFactorCard />
    </QueryClientProvider>,
  );
}

describe("TwoFactorCard", () => {
  it("状态读取失败时显示状态未知而非未开启", async () => {
    // 旧实现在查询失败时回落到「未开启」：管理员会误判账号未受保护并重复配置。
    statusMock.mockRejectedValue(new Error("network down"));
    renderCard();
    expect(await screen.findByText("状态未知")).toBeInTheDocument();
    expect(screen.queryByText("未开启")).not.toBeInTheDocument();
    expect(screen.getByRole("alert")).toHaveTextContent("无法读取两步验证状态");
  });

  it("开启成功后状态查询被失效并展示已开启", async () => {
    const user = userEvent.setup();
    statusMock.mockResolvedValueOnce({ enabled: false }).mockResolvedValue({ enabled: true });
    setupMock.mockResolvedValue(setup);
    enableMock.mockResolvedValue({ enabled: true });
    renderCard();
    await user.click(await screen.findByRole("button", { name: /配置并开启两步验证/ }));
    await user.type(await screen.findByPlaceholderText("000000"), "123456");
    await user.click(screen.getByRole("button", { name: /确认并开启/ }));
    expect(await screen.findByText("已开启")).toBeInTheDocument();
    // 密钥区块随之收起：mutationFn 的守卫在界面层无入口。
    expect(screen.queryByPlaceholderText("000000")).not.toBeInTheDocument();
  });

  it("校验失败时展示服务端错误信息", async () => {
    const user = userEvent.setup();
    statusMock.mockResolvedValue({ enabled: false });
    setupMock.mockResolvedValue(setup);
    enableMock.mockRejectedValue(
      new ApiError({ status: 400, errorCode: "two_factor_code_invalid", message: "动态验证码错误，请重试" }),
    );
    renderCard();
    await user.click(await screen.findByRole("button", { name: /配置并开启两步验证/ }));
    await user.type(await screen.findByPlaceholderText("000000"), "123456");
    await user.click(screen.getByRole("button", { name: /确认并开启/ }));
    expect(await screen.findByRole("alert")).toHaveTextContent("动态验证码错误，请重试");
  });
});

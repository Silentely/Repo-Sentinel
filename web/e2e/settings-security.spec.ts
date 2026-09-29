import { expect, test } from "@playwright/test";

import { adminPassword, adminUsername, ensureAuthenticated, generateTOTP } from "./helpers";

test.describe("设置与安全防护", () => {
  test("未登录状态受保护路由重定向拦截", async ({ browser, baseURL, page }) => {
    // 确保管理员账号已就绪（防止未初始化实例重定向至 /setup）
    await ensureAuthenticated(page);

    // 创建一个干净的无 Cookie 访客上下文
    const incognitoContext = await browser.newContext();
    const guestPage = await incognitoContext.newPage();

    // 尝试直接访问受保护的设置页
    await guestPage.goto(`${baseURL}/settings`);
    await guestPage.waitForURL("**/login**", { timeout: 15_000 });
    await expect(guestPage.getByRole("button", { name: "登录" })).toBeVisible();
    await expect(guestPage.getByLabel("用户名")).toBeVisible();

    // 尝试直接访问受保护的仓库管理页
    await guestPage.goto(`${baseURL}/repos`);
    await guestPage.waitForURL("**/login**", { timeout: 15_000 });
    await expect(guestPage.getByRole("button", { name: "登录" })).toBeVisible();
    await expect(guestPage.getByLabel("用户名")).toBeVisible();

    await incognitoContext.close();
  });

  test("两步验证 (2FA / TOTP) 配置引导展开、数字校验边界、错误口令拦截与取消关闭", async ({
    page,
    context,
  }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await ensureAuthenticated(page);
    await page.goto("/settings");

    const twoFactorSection = page.locator("section.channel-form").filter({ hasText: "两步验证 (2FA / TOTP)" });
    await expect(twoFactorSection).toBeVisible({ timeout: 15_000 });

    // 1. 验证初始状态：未开启
    await expect(twoFactorSection.getByText("未开启")).toBeVisible();
    const setupBtn = twoFactorSection.getByRole("button", { name: "配置并开启两步验证" });
    await expect(setupBtn).toBeVisible();

    // 2. 点击配置并开启两步验证，展开引导步骤
    await setupBtn.click();
    await expect(twoFactorSection.getByText("第 1 步：在验证器中绑定密钥")).toBeVisible();
    await expect(twoFactorSection.getByText("第 2 步：输入 6 位动态验证码完成激活")).toBeVisible();

    // 密钥展示与复制按钮
    const copySecretBtn = twoFactorSection.getByRole("button", { name: /复制/ });
    await expect(copySecretBtn).toBeVisible();
    await copySecretBtn.click({ force: true });
    await expect(twoFactorSection.getByText("已复制")).toBeVisible();

    // 3. 校验输入边界：不足 6 位时确认按钮保持禁用
    const codeInput = twoFactorSection.getByPlaceholder("000000");
    const confirmBtn = twoFactorSection.getByRole("button", { name: "确认并开启" });
    await codeInput.fill("12345");
    await expect(confirmBtn).toBeDisabled();

    // 非数字字符自动过滤
    await codeInput.fill("abc999");
    await expect(codeInput).toHaveValue("999");
    await expect(confirmBtn).toBeDisabled();

    // 4. 输入 6 位错误口令触发校验异常拦截
    await codeInput.fill("000000");
    await expect(confirmBtn).toBeEnabled();
    await confirmBtn.click({ force: true });

    // 捕获校验失败提示
    await expect(twoFactorSection.getByRole("alert")).toBeVisible();
    await expect(twoFactorSection.getByText(/请求内容未通过校验|验证码校验失败|无效/)).toBeVisible();

    // 5. 点击取消恢复初始状态，避免状态残留
    const cancelBtn = twoFactorSection.getByRole("button", { name: "取消" });
    await cancelBtn.click({ force: true });
    await expect(twoFactorSection.getByText("第 1 步：在验证器中绑定密钥")).toBeHidden();
    await expect(setupBtn).toBeVisible();
    await expect(twoFactorSection.getByText("未开启")).toBeVisible();
  });

  test("真实 TOTP 口令激活、登出拦截挑战、动态验证码核验登录与安全关闭闭环", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/settings");

    const twoFactorSection = page.locator("section.channel-form").filter({ hasText: "两步验证 (2FA / TOTP)" });
    await expect(twoFactorSection).toBeVisible({ timeout: 15_000 });

    // 若当前为已开启状态，先重置关闭
    if (await twoFactorSection.getByRole("button", { name: "关闭两步验证" }).isVisible()) {
      await twoFactorSection.getByRole("button", { name: "关闭两步验证" }).click();
      await twoFactorSection.getByPlaceholder("请输入当前管理员密码").fill(adminPassword);
      await twoFactorSection.getByRole("button", { name: "确认关闭" }).click();
      await expect(twoFactorSection.getByText("未开启")).toBeVisible();
    }

    // 1. 点击开启并获取服务端生成的 Base32 Secret
    await twoFactorSection.getByRole("button", { name: "配置并开启两步验证" }).click();
    const secretCodeEl = twoFactorSection.locator("code");
    await expect(secretCodeEl).toBeVisible();
    const secret = (await secretCodeEl.textContent())?.trim() || "";
    expect(secret.length).toBeGreaterThanOrEqual(16);

    // 2. 根据 RFC 6238 实时计算当前 6 位动态口令并提交激活
    const totpCode = generateTOTP(secret);
    const codeInput = twoFactorSection.getByPlaceholder("000000");
    await codeInput.fill(totpCode);
    await twoFactorSection.getByRole("button", { name: "确认并开启" }).click({ force: true });

    // 验证激活成功反馈与状态更新
    await expect(twoFactorSection.getByRole("status")).toHaveText(/二步验证已成功开启/);
    await expect(twoFactorSection.getByText("已开启")).toBeVisible();

    // 3. 退出登录
    const logoutBtn = page.getByRole("button", { name: /退出/ });
    await logoutBtn.click({ force: true });
    await page.waitForURL("**/login**", { timeout: 15_000 });

    // 4. 输入管理员账密登录，触发 2FA 挑战
    await page.getByLabel("用户名").fill(adminUsername);
    await page.getByLabel("密码").fill(adminPassword);
    await page.getByRole("button", { name: "登录" }).click();

    // 断言界面平滑切换至 2FA 动态口令输入
    const passcodeInput = page.locator("#login-passcode");
    await expect(passcodeInput).toBeVisible({ timeout: 10_000 });
    await expect(page.getByRole("button", { name: "验证并登录" })).toBeVisible();

    // 5. 填入实时 TOTP 口令完成二次认证
    const loginTotp = generateTOTP(secret);
    await passcodeInput.fill(loginTotp);
    await page.getByRole("button", { name: "验证并登录" }).click();

    // 断言成功回到主仪表盘
    await expect(page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" })).toBeVisible({
      timeout: 15_000,
    });

    // 6. 清理还原：回到设置页关闭 2FA，保证后续测试与其他用例幂等
    await page.goto("/settings");
    const cleanupSection = page.locator("section.channel-form").filter({ hasText: "两步验证 (2FA / TOTP)" });
    await expect(cleanupSection).toBeVisible();
    await cleanupSection.getByRole("button", { name: "关闭两步验证" }).click({ force: true });
    await cleanupSection.getByPlaceholder("请输入当前管理员密码").fill(adminPassword);
    await cleanupSection.getByRole("button", { name: "确认关闭" }).click({ force: true });
    await expect(cleanupSection.getByText("未开启")).toBeVisible();
  });
});

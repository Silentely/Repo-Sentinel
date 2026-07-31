import { expect, type Page } from "@playwright/test";

/**
 * 实例为单管理员模型：每个 e2e 服务只允许创建一个管理员。
 * 各用例文件统一经由守卫入口复用同一组凭据，谁先执行谁负责完成首次设置。
 */
export const adminUsername = "Repo Admin";
export const adminPassword = "安全管理员密码一二三四五六";

/** 进入应用：按当前实例状态完成首次设置或登录，最终停在仪表盘。 */
export async function ensureAuthenticated(page: Page) {
  await page.goto("/");

  // 未初始化的实例会经过 "/" → /login → /setup 跳转链，已初始化则停在 /login。
  // 轮询直到页面内容与 URL 一致，避免在跳转链中间态上误填表单。
  const setupHeading = page.getByRole("heading", { name: "创建唯一管理员" });
  const loginButton = page.getByRole("button", { name: "登录" });
  await expect(async () => {
    if (await setupHeading.isVisible()) {
      expect(page.url()).toMatch(/\/setup$/);
    } else {
      expect(await loginButton.isVisible()).toBe(true);
      expect(page.url()).toMatch(/\/login$/);
    }
  }).toPass({ timeout: 15_000 });

  if (page.url().endsWith("/setup")) {
    await page.getByRole("textbox", { name: "用户名" }).fill(adminUsername);
    await page.getByLabel("密码", { exact: true }).fill(adminPassword);
    await page.getByLabel("确认密码").fill(adminPassword);
    await page.getByRole("button", { name: "创建管理员" }).click();
  } else {
    await page.getByRole("textbox", { name: "用户名" }).fill(adminUsername);
    await page.getByLabel("密码", { exact: true }).fill(adminPassword);
    await loginButton.click();
  }

  await expect(page).toHaveURL(/\/$/);
}

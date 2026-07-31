import { expect, test } from "@playwright/test";

import { adminUsername as username, adminPassword as password } from "./helpers";

test("首次设置、退出、错误凭据与重新登录形成完整认证旅程", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole("heading", { name: "创建唯一管理员" })).toBeVisible();

  const theme = page.getByRole("combobox", { name: "主题" });
  await theme.selectOption("dark");
  await page.reload();
  await expect(page.getByRole("combobox", { name: "主题" })).toHaveValue("dark");
  await expect(page.locator("html")).toHaveClass(/dark/);

  await page.getByRole("textbox", { name: "用户名" }).fill(username);
  await page.getByLabel("密码", { exact: true }).fill("太短");
  await page.getByLabel("确认密码").fill("并不相同");
  await page.getByRole("button", { name: "创建管理员" }).click();
  await expect(page.getByText("密码至少需要 12 个 Unicode 字符。")).toBeVisible();
  await expect(page.getByText("两次输入的密码不一致。")).toBeVisible();
  await expect(page).toHaveURL(/\/setup$/);

  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByLabel("确认密码").fill(password);
  await page.getByRole("button", { name: "创建管理员" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" })).toBeVisible();

  await page.getByRole("button", { name: "退出" }).click();
  await expect(page).toHaveURL(/\/login$/);

  await page.evaluate(() => {
    if (document.activeElement instanceof HTMLElement) {
      document.activeElement.blur();
    }
  });
  await page.keyboard.press("Tab");
  await expect(page.getByRole("combobox", { name: "主题" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("textbox", { name: "用户名" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByLabel("密码", { exact: true })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("button", { name: "登录" })).toBeFocused();
  await page.keyboard.press("Tab");
  await expect(page.getByRole("link", { name: "使用 CLI 重置密码" })).toBeFocused();

  await page.getByRole("textbox", { name: "用户名" }).fill(username);
  await page.getByLabel("密码", { exact: true }).fill("错误密码一二三四五六");
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page.getByRole("alert")).toContainText("用户名或密码不正确");
  await expect(page.getByRole("alert")).toContainText("invalid_credentials");

  await page.getByLabel("密码", { exact: true }).fill(password);
  await page.getByRole("button", { name: "登录" }).click();
  await expect(page).toHaveURL(/\/$/);
  await expect(page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" })).toBeVisible();
  await expect(page.getByRole("combobox", { name: "主题" })).toHaveValue("dark");
});

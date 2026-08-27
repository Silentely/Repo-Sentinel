import { expect, test } from "@playwright/test";

import { adminUsername as username, adminPassword as password } from "./helpers";

test("首次设置、退出、错误凭据与重新登录形成完整认证旅程", async ({ page }) => {
  await page.goto("/");
  await expect(page).toHaveURL(/\/setup$/);
  await expect(page.getByRole("heading", { name: "创建唯一管理员" })).toBeVisible();

  const theme = page.getByRole("combobox", { name: "主题" });
  await theme.selectOption("dark");
  await page.reload();
  // 主题持久化按存储层验证：移动端顶栏会隐藏选择器控件（display:none 不在可访问树）。
  await expect(page.locator("html")).toHaveClass(/dark/);
  expect(await page.evaluate(() => window.localStorage.getItem("reposentinel-theme"))).toBe("dark");

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

  // 页首 Tab 序（DOM 顺序）：GitHub 仓库链接 → 主题 → 用户名 → 密码 → 登录 → CLI 链接。
  // 显式从首元素聚焦起步：blur() 后浏览器按上次焦点锚点续序，起点不稳定。
  await page.getByRole("link", { name: "GitHub 仓库" }).focus();
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
  await expect(page.locator("html")).toHaveClass(/dark/);
});

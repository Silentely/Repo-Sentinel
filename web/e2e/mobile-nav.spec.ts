import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

// 桌面项目仍保留 901px 以上的常驻侧边栏，本用例只针对移动端视口。
test.skip(({ isMobile }) => !isMobile, "仅校验移动端视口");

test("移动端抽屉导航：可开启、可跳转、多种方式关闭", async ({ page }) => {
  await ensureAuthenticated(page);

  const sidebar = page.locator("#app-sidebar");
  const menuButton = page.getByRole("button", { name: "打开导航菜单" });

  // 移动端侧边栏默认离屏隐藏，菜单按钮接管导航入口。
  // 首个交互元素等待放宽：冷启动（浏览器+查询首载）可能超过默认 5s。
  await expect(menuButton).toBeVisible({ timeout: 15_000 });
  await expect(sidebar).toBeHidden();

  await menuButton.click();
  await expect(sidebar).toBeVisible();
  await expect(sidebar.getByRole("link", { name: "仪表盘" })).toBeVisible();
  await expect(sidebar.getByRole("link", { name: "关于" })).toBeVisible();
  await expect(sidebar.getByRole("link", { name: "设置" })).toBeVisible();

  // 点链接跳转后抽屉自动收起。
  await sidebar.getByRole("link", { name: "仓库管理" }).click();
  await expect(page).toHaveURL(/\/repos$/);
  await expect(sidebar).toBeHidden();
  await expect(page.locator(".app-topbar__title--mobile")).toHaveText("仓库管理");
  await expect(menuButton).toBeFocused();

  // Escape 关闭。
  await menuButton.click();
  await expect(sidebar).toBeVisible();
  await page.keyboard.press("Escape");
  await expect(sidebar).toBeHidden();

  // 遮罩点击关闭。抽屉宽 300px，点击右侧遮罩空白处才能命中遮罩。
  await menuButton.click();
  await expect(sidebar).toBeVisible();
  await page.locator(".app-scrim").click({ position: { x: 390, y: 400 } });
  await expect(sidebar).toBeHidden();

  // 抽屉内关闭按钮。
  await menuButton.click();
  await sidebar.getByRole("button", { name: "收起导航菜单" }).click();
  await expect(sidebar).toBeHidden();
});

test("移动端各主要页面无横向溢出", async ({ page }) => {
  await ensureAuthenticated(page);

  for (const path of [
    "/",
    "/repos",
    "/issues",
    "/pull-requests",
    "/actions",
    "/security",
    "/notifications",
    "/notifications/outbox",
    "/github",
    "/about",
    "/settings",
  ]) {
    await page.goto(path);
    await page.waitForLoadState("networkidle");
    const overflow = await page.evaluate(
      () => document.documentElement.scrollWidth - document.documentElement.clientWidth,
    );
    expect(overflow, `${path} 不应出现横向滚动条`).toBeLessThanOrEqual(1);
  }
});

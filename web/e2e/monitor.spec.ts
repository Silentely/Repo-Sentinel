import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

test("设置页保存后刷新回读一致（偏好分区 round-trip）", async ({ page }) => {
  await ensureAuthenticated(page);
  await page.goto("/settings");

  // 勾选「启用每周报告」并保存该分区。
  const weekly = page.getByRole("checkbox", { name: "启用每周报告" });
  await expect(weekly).toBeVisible();
  if (!(await weekly.isChecked())) {
    await weekly.click();
  }
  await expect(weekly).toBeChecked();
  await page.getByRole("button", { name: "保存偏好" }).click();
  await expect(page.getByRole("status").filter({ hasText: "已保存" }).first()).toBeVisible();

  // 刷新后从服务端回读：勾选状态必须保留（锁定跨区块保存不回滚的回归）。
  await page.reload();
  await expect(page.getByRole("checkbox", { name: "启用每周报告" })).toBeChecked();

  // 还原：取消勾选并保存，避免影响其它用例的默认前提。
  await page.getByRole("checkbox", { name: "启用每周报告" }).click();
  await page.getByRole("button", { name: "保存偏好" }).click();
  await expect(page.getByRole("status").filter({ hasText: "已保存" }).first()).toBeVisible();
  await page.reload();
  await expect(page.getByRole("checkbox", { name: "启用每周报告" })).not.toBeChecked();
});

test("仪表盘首屏渲染关键区块且控制台无错误", async ({ page }) => {
  const consoleErrors: string[] = [];
  page.on("console", (msg) => {
    if (msg.type() === "error") consoleErrors.push(msg.text());
  });
  await ensureAuthenticated(page);

  await expect(page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" })).toBeVisible();
  await expect(page.getByLabel("关键指标")).toBeVisible();
  // 最近事件与投递面板（标题锚定，避免与侧边栏同名入口撞 strict mode）。
  await expect(page.getByRole("heading", { name: "最近事件" })).toBeVisible();
  // 过滤浏览器自身的良性提示（CSP 无 script-src 时的 fallback note、资源 404）。
  expect(
    consoleErrors.filter(
      (e) => !e.includes("Failed to load resource") && !e.includes("'script-src' was not explicitly set"),
    ),
  ).toEqual([]);
});

test("投递记录筛选为空时空态提供「清除筛选」并可点击", async ({ page }) => {
  await ensureAuthenticated(page);
  await page.goto("/notifications/outbox?status=dead");

  await expect(page.getByRole("heading", { name: "没有投递记录" })).toBeVisible();
  // 空态操作区内的「清除筛选」（工具栏另有同名按钮，strict mode 需锚定容器）。
  const clear = page.locator(".empty-state").getByRole("button", { name: "清除筛选" });
  await expect(clear).toBeVisible();
  await clear.click();
  await expect(page).toHaveURL(/\/notifications\/outbox$/);
});

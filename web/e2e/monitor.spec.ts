import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

test("设置页保存后刷新回读一致（偏好分区 round-trip）", async ({ page }) => {
  await ensureAuthenticated(page);
  await page.goto("/settings");

  const weekly = page.getByRole("checkbox", { name: "启用每周报告" });
  // 首个交互元素等待放宽：冷启动（浏览器+查询首载）可能超过默认 5s。
  await expect(weekly).toBeVisible({ timeout: 15_000 });
  const initiallyChecked = await weekly.isChecked();
  try {
    if (!initiallyChecked) {
      await weekly.click();
    }
    await expect(weekly).toBeChecked();
    await page.getByRole("button", { name: "保存偏好" }).click();
    await expect(page.getByRole("status").filter({ hasText: "已保存" }).first()).toBeVisible();

    // 刷新后从服务端回读：勾选状态必须保留（锁定跨区块保存不回滚的回归）。
    await page.reload();
    await expect(page.getByRole("checkbox", { name: "启用每周报告" })).toBeChecked();
  } finally {
    // 还原初始状态：断言失败也要还原，否则污染共享实例与下次运行。
    // 还原本身失败不覆盖原始断言错误。
    await page.reload();
    const restore = page.getByRole("checkbox", { name: "启用每周报告" });
    if ((await restore.isChecked()) !== initiallyChecked) {
      await restore.click();
      await page.getByRole("button", { name: "保存偏好" }).click().catch(() => {});
    }
  }
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

  // 依赖「实例无 dead 投递」的隐含前提：未来用例引入失败投递时会破坏，
  // 先经 API 清理（忽略错误），再断言空态。
  await page.evaluate(async () => {
    const csrf = (document.cookie.match(/(?:^|; )reposentinel_csrf=([^;]+)/) || [])[1] || "";
    const list = await fetch("/api/v1/notifications/outbox?status=dead&per_page=100", { credentials: "include" }).then((r) => r.json());
    await Promise.all(
      (list.items ?? []).map((it: { id: string }) =>
        fetch(`/api/v1/notifications/outbox/${it.id}/retry`, {
          method: "POST", credentials: "include",
          headers: { "Content-Type": "application/json", "X-CSRF-Token": decodeURIComponent(csrf) },
          body: "{}",
        }).catch(() => undefined),
      ),
    );
  });
  await page.reload();

  await expect(page.getByRole("heading", { name: "没有投递记录" })).toBeVisible();
  // 空态操作区内的「清除筛选」（工具栏另有同名按钮，strict mode 需锚定容器）。
  const clear = page.locator(".empty-state").getByRole("button", { name: "清除当前所有筛选条件" });
  await expect(clear).toBeVisible();
  await clear.click();
  await expect(page).toHaveURL(/\/notifications\/outbox$/);
});

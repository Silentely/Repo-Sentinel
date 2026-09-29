import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

test.describe("工作项批量操作与实时事件流", () => {
  test("工作项列表多选、悬浮操作栏展示、取消选择与批量忽略", async ({ page }) => {
    await ensureAuthenticated(page);

    // Mock work items 数据
    await page.route("**/api/v1/work-items?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("kind") === "issue") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            items: [
              {
                id: "wi-issue-101",
                kind: "issue",
                number: 101,
                title: "bug: memory leak in worker goroutine",
                state: "open",
                html_url: "https://github.com/Silentely/Repo-Sentinel/issues/101",
                author: "alice",
                source_updated_at: new Date().toISOString(),
                ignored: false,
              },
              {
                id: "wi-issue-102",
                kind: "issue",
                number: 102,
                title: "feature: support batch ignore",
                state: "open",
                html_url: "https://github.com/Silentely/Repo-Sentinel/issues/102",
                author: "bob",
                source_updated_at: new Date().toISOString(),
                ignored: false,
              },
            ],
            total: 2,
            page: 1,
            per_page: 50,
          }),
        });
        return;
      }
      await route.continue();
    });

    let batchIgnoreCalled = false;
    let batchIgnorePayload: unknown = null;
    await page.route("**/api/v1/work-items/batch-ignore", async (route) => {
      batchIgnoreCalled = true;
      batchIgnorePayload = route.request().postDataJSON();
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({ updated_count: 2, ignored: true }),
      });
    });

    await page.goto("/issues");

    // 1. 验证工作项渲染
    await expect(page.getByText("#101 bug: memory leak in worker goroutine")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText("#102 feature: support batch ignore")).toBeVisible();

    // 2. 初始状态下无批量操作栏
    const actionBar = page.getByRole("region", { name: "批量操作栏" });
    await expect(actionBar).not.toBeVisible();

    // 3. 勾选第 1 个工作项
    const checkbox1 = page.getByRole("checkbox", { name: "选择 #101" });
    await checkbox1.click();
    await expect(actionBar).toBeVisible();
    await expect(actionBar.getByText("已选择 1 项")).toBeVisible();

    // 4. 勾选第 2 个工作项
    const checkbox2 = page.getByRole("checkbox", { name: "选择 #102" });
    await checkbox2.click();
    await expect(actionBar.getByText("已选择 2 项")).toBeVisible();

    // 5. 点击取消选择：操作栏消失且勾选框复位
    await actionBar.getByRole("button", { name: "取消选择" }).click();
    await expect(actionBar).not.toBeVisible();
    await expect(checkbox1).not.toBeChecked();
    await expect(checkbox2).not.toBeChecked();

    // 6. 重新勾选并触发批量忽略
    await checkbox1.click();
    await checkbox2.click();
    await expect(actionBar.getByText("已选择 2 项")).toBeVisible();

    await actionBar.getByRole("button", { name: "批量忽略" }).click();

    await expect(async () => {
      expect(batchIgnoreCalled).toBe(true);
    }).toPass({ timeout: 5_000 });

    expect(batchIgnorePayload).toEqual({
      ids: ["wi-issue-101", "wi-issue-102"],
      ignored: true,
    });
  });

  test("登录态下页面加载时自动连接 SSE 实时事件流", async ({ page }) => {
    await ensureAuthenticated(page);

    const sseRequestPromise = page.waitForRequest(
      (req) => req.url().includes("/api/v1/events/stream"),
      { timeout: 15_000 },
    );

    await page.reload();
    const req = await sseRequestPromise;
    expect(req.url()).toContain("/api/v1/events/stream");
  });
});

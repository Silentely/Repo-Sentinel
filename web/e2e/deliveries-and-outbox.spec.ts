import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

test.describe("投递记录 (Outbox) 与 Webhook 检查回放", () => {
  test("通知投递记录：状态与渠道筛选、详情抽屉排障及失败重试", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);

    let retryCalled = false;
    let retryAllCalled = false;

    // 使用正则精确拦截所有 outbox 相关请求（含子路径 /retry 及 /retry-dead）
    await page.route(/\/api\/v1\/notifications\/outbox/, async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "POST" && url.pathname.includes("/retry-dead")) {
        retryAllCalled = true;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ retried: 2 }),
        });
        return;
      }
      if (route.request().method() === "POST" && url.pathname.includes("/retry")) {
        retryCalled = true;
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ queued: true }),
        });
        return;
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: "out-mock-1",
              channel_id: "ch-1",
              channel_type: "telegram",
              status: "dead",
              title: "生产环境登录异常告警",
              attempt_count: 8,
              last_error_code: "telegram_status_500",
              html_url: "https://github.com/Silentely/Repo-Sentinel/issues/101",
              body_text: "<b>🔴 生产环境告警｜服务响应延迟超标</b>\n📦 仓库：<code>Silentely/Repo-Sentinel</code>",
              created_at: "2026-09-29T04:00:00Z",
              updated_at: "2026-09-29T04:15:00Z",
            },
          ],
          page: 1,
          per_page: 50,
          total: 1,
        }),
      });
    });

    await ensureAuthenticated(page);
    await page.goto("/notifications/outbox");

    // 1. 验证标题与基础工具栏渲染
    await expect(page.getByRole("heading", { name: "投递记录" })).toBeVisible({ timeout: 15_000 });

    // 2. 状态筛选：点击「投递失败」，URL 与激活态同步
    const deadFilterBtn = page.getByRole("button", { name: "投递失败" });
    await expect(deadFilterBtn).toBeVisible();
    await deadFilterBtn.click();
    await expect(page).toHaveURL(/status=dead/);

    // 3. 渠道筛选：切换至「Telegram」
    const channelSelect = page.getByRole("combobox", { name: "按渠道筛选" });
    await expect(channelSelect).toBeVisible();
    await channelSelect.selectOption("telegram");
    await expect(page).toHaveURL(/channel=telegram/);

    // 4. 验证投递失败记录渲染
    const itemCard = page.locator("ul.event-list li").first();
    await expect(itemCard).toBeVisible();
    await expect(itemCard).toContainText("生产环境登录异常告警");
    await expect(itemCard).toContainText("投递失败");
    await expect(itemCard).toContainText("telegram_status_500");

    // 5. 点击记录查看详情打开排障详情抽屉
    const detailBtn = itemCard.getByRole("button", { name: /查看投递详情/ });
    await detailBtn.click();
    const detailDrawer = page.getByRole("dialog", { name: "投递详情" });
    await expect(detailDrawer).toBeVisible();
    await expect(detailDrawer).toContainText("生产环境登录异常告警");
    await expect(detailDrawer).toContainText("生产环境告警｜服务响应延迟超标");

    // 键盘 Escape 优雅关闭详情抽屉
    await page.keyboard.press("Escape");
    await expect(detailDrawer).toBeHidden();

    // 6. 单条重试交互
    const retryBtn = itemCard.getByRole("button", { name: /重试投递/ });
    await expect(retryBtn).toBeVisible();
    await retryBtn.click();
    await expect.poll(() => retryCalled).toBe(true);

    // 7. 批量重试交互：等待操作就绪，弹窗二次确认
    const batchRetryBtn = page.getByRole("button", { name: /重试全部失败/ });
    await expect(batchRetryBtn).toBeVisible();
    await expect(batchRetryBtn).toBeEnabled();
    await batchRetryBtn.click({ force: true });

    const confirmModal = page.getByRole("dialog", { name: "重新排队全部失败投递" });
    await expect(confirmModal).toBeVisible();
    await confirmModal.getByRole("button", { name: "重新排队" }).click();
    await expect(confirmModal).toBeHidden();
    await expect.poll(() => retryAllCalled).toBe(true);
    await expect(page.getByRole("status").filter({ hasText: /已重新排队/ })).toBeVisible();
  });

  test("Webhook 检查与回放：列表呈现、详情 Inspector 弹窗与重放", async ({ page }) => {
    // 使用正则精确拦截所有 webhook-deliveries 请求（含列表、详情与 /replay）
    await page.route(/\/api\/v1\/webhook-deliveries/, async (route) => {
      const url = new URL(route.request().url());
      if (route.request().method() === "POST" && url.pathname.endsWith("/replay")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({ delivery_id: "gh-del-e2e-888" }),
        });
        return;
      }
      if (url.pathname.includes("/del-uuid-1")) {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            delivery: {
              id: "del-uuid-1",
              delivery_id: "gh-del-e2e-888",
              event_type: "push",
              action: "",
              repository_full_name: "Silentely/Repo-Sentinel",
              status: "processed",
              received_at: "2026-09-29T04:20:00Z",
            },
            payload_json: {
              ref: "refs/heads/main",
              commits: [{ message: "fix: update e2e security tests" }],
            },
          }),
        });
        return;
      }

      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: "del-uuid-1",
              delivery_id: "gh-del-e2e-888",
              event_type: "push",
              action: "",
              repository_full_name: "Silentely/Repo-Sentinel",
              status: "processed",
              received_at: "2026-09-29T04:20:00Z",
            },
          ],
          page: 1,
          per_page: 20,
          total: 1,
        }),
      });
    });

    await ensureAuthenticated(page);
    await page.goto("/webhooks");

    // 1. 验证标题与列表加载
    await expect(page.getByRole("heading", { name: "Webhook 投递检查与回放" })).toBeVisible({ timeout: 15_000 });
    const row = page.locator("li.channel-row").filter({ hasText: "gh-del-e2e-888" });
    await expect(row).toBeVisible();
    await expect(row).toContainText("push");
    await expect(row).toContainText("Silentely/Repo-Sentinel");
    await expect(row).toContainText("已处理");

    // 2. 点击「检查」打开 Inspector 弹窗
    const inspectBtn = row.getByRole("button", { name: "检查" });
    await inspectBtn.click();

    const inspectorDialog = page.getByRole("dialog", { name: "Webhook 载荷检查 (Inspector)" });
    await expect(inspectorDialog).toBeVisible();
    await expect(inspectorDialog).toContainText("gh-del-e2e-888");
    await expect(inspectorDialog).toContainText("refs/heads/main");

    // 键盘 Escape 键关闭弹窗
    await page.keyboard.press("Escape");
    await expect(inspectorDialog).toBeHidden();

    // 3. 点击「重放」按钮触发回放
    const replayBtn = row.getByRole("button", { name: "重放" });
    await replayBtn.click();
    await expect(page.getByRole("status").filter({ hasText: "Webhook 已重放并重新排队处理（ID: gh-del-e2e-888）" })).toBeVisible();
  });
});

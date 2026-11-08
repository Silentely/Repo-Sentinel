import { expect, test } from "@playwright/test";

test.describe("Telegram Mini App (TMA)", () => {
  test("C4-1: 未注入 Telegram SDK 时，显示占位引导页且零 API 请求", async ({ page }) => {
    const tmaRequests: string[] = [];
    page.on("request", (req) => {
      const url = req.url();
      if (url.includes("/api/v1/tma")) {
        tmaRequests.push(url);
      }
    });

    await page.goto("/tma/dashboard");

    // 验证占位引导页标题与引导文本可见
    await expect(page.getByRole("heading", { name: "请在 Telegram 客户端中打开" })).toBeVisible({
      timeout: 10_000,
    });
    await expect(
      page.getByText("Repo-Sentinel Mini App 专为 Telegram 移动端与桌面端设计。"),
    ).toBeVisible();

    // 验证入口被严格守卫，未向 TMA 后端发送任何 API 请求
    expect(tmaRequests).toHaveLength(0);
  });

  test("C4-2: 注入 Telegram SDK stub 后成功加载 Mini App 仪表盘与 KPI", async ({ page }) => {
    // 注入 Telegram WebApp SDK stub
    await page.addInitScript(() => {
      (window as any).Telegram = {
        WebApp: {
          initData: "query_id=AAHd&user=%7B%22id%22%3A12345%7D&auth_date=1620000000&hash=mockhash",
          initDataUnsafe: {
            user: { id: 12345, first_name: "TestUser" },
          },
          themeParams: {
            bg_color: "#0f172a",
            text_color: "#f8fafc",
            secondary_bg_color: "#1e293b",
            button_color: "#38bdf8",
            button_text_color: "#ffffff",
          },
          isExpanded: true,
          ready: () => {},
          expand: () => {},
          close: () => {},
          onEvent: () => {},
          offEvent: () => {},
        },
      };
    });

    // Mock dashboard API 响应
    await page.route("**/api/v1/tma/dashboard", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          monitored_repos_count: 8,
          pending_work_items_count: 3,
          webhook_deliveries_total: 120,
          webhook_deliveries_success: 118,
          webhook_delivery_rate: 0.983,
          quiet_hours_enabled: false,
          quiet_hours_start: "22:00",
          quiet_hours_end: "08:00",
          quiet_hours_tz: "Asia/Shanghai",
          work_items: [],
        }),
      });
    });

    await page.goto("/tma/dashboard");

    await expect(page.getByRole("heading", { name: "RepoSentinel" })).toBeVisible({
      timeout: 10_000,
    });
    await expect(page.getByText("移动端告警与监控看板")).toBeVisible();
    await expect(page.getByText("纳管仓库")).toBeVisible();
    await expect(page.getByText("待办告警")).toBeVisible();
    await expect(page.getByText("8")).toBeVisible();
  });

  test("C4-3: 动作执行遇到 HTTP 409 时呈现告警反馈与防重复锁定", async ({ page }) => {
    await page.addInitScript(() => {
      (window as any).Telegram = {
        WebApp: {
          initData: "query_id=AAHd&user=%7B%22id%22%3A12345%7D&auth_date=1620000000&hash=mockhash",
          initDataUnsafe: {
            user: { id: 12345, first_name: "TestUser" },
          },
          themeParams: {},
          isExpanded: true,
          ready: () => {},
          expand: () => {},
          close: () => {},
          onEvent: () => {},
          offEvent: () => {},
        },
      };
    });

    // Mock event detail
    await page.route("**/api/v1/tma/events/evt-playwright-409", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          event: {
            id: "evt-playwright-409",
            kind: "workflow_run",
            action: "completed",
            title: "CI / Build and Test Failed",
            severity: "error",
            actor: "developer",
            html_url: "https://example.invalid/repo/actions/runs/1",
            occurred_at: "2026-11-08T06:00:00Z",
            workflow_run_id: 1001,
          },
          repository: {
            id: "repo-1",
            full_name: "acme/sentinel",
            owner: "acme",
            name: "sentinel",
            is_private: true,
            is_archived: false,
            monitor_enabled: true,
            html_url: "https://example.invalid/repo",
          },
        }),
      });
    });

    // Mock logs
    await page.route("**/api/v1/tma/events/evt-playwright-409/logs", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          logs: "FAIL: TestMain failed",
          truncated: false,
          job_id: 1,
          job_name: "test",
        }),
      });
    });

    // Mock action with HTTP 409 Conflict
    await page.route("**/api/v1/tma/actions", async (route) => {
      await route.fulfill({
        status: 409,
        contentType: "application/json",
        body: JSON.stringify({
          error_code: "conflict",
          message: "动作正在执行中，请勿重复操作",
          details: {
            status: "pending",
            reason: "action_in_progress",
            action: "workflow_rerun",
          },
        }),
      });
    });

    await page.goto("/tma/events/evt-playwright-409");

    const rerunButton = page.getByRole("button", { name: "🔄 重新运行" });
    await expect(rerunButton).toBeVisible({ timeout: 10_000 });
    await expect(rerunButton).toBeEnabled();

    // 点击操作
    await rerunButton.click();

    // 断言 409 警告反馈信息出现
    await expect(page.getByText("该动作已在后台处理中，请勿重复操作")).toBeVisible({
      timeout: 10_000,
    });

    // 断言按钮被锁定禁用 (防并发重复提交)
    await expect(rerunButton).toBeDisabled();
  });
});

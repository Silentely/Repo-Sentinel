import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

test.describe("Pull Requests 审查与维护者裁决", () => {
  test("PR 列表呈现、AI 审查报告展开、维护者合并裁决与敏感资产嗅探预警", async ({ page, context }) => {
    await context.grantPermissions(["clipboard-read", "clipboard-write"]);
    await ensureAuthenticated(page);

    // Mock PR 列表数据
    await page.route("**/api/v1/work-items?*", async (route) => {
      const url = new URL(route.request().url());
      if (url.searchParams.get("kind") === "pull_request") {
        await route.fulfill({
          status: 200,
          contentType: "application/json",
          body: JSON.stringify({
            items: [
              {
                id: "wi-pr-block",
                kind: "pull_request",
                number: 101,
                title: "refactor: update authentication & deploy configs",
                state: "open",
                html_url: "https://github.com/Silentely/Repo-Sentinel/pull/101",
                author: "contributor-bob",
                source_updated_at: new Date().toISOString(),
                review_state: "pending",
                check_status: "passed",
                checks_total: 5,
                checks_passed: 5,
              },
              {
                id: "wi-pr-err",
                kind: "pull_request",
                number: 102,
                title: "feat: add third-party payment integration",
                state: "open",
                html_url: "https://github.com/Silentely/Repo-Sentinel/pull/102",
                author: "contributor-alice",
                source_updated_at: new Date().toISOString(),
                review_state: "pending",
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

    // Mock PR #101 的 AI 审查详情：包含近期核心功能 maintainer_verdict 与 sensitive_assets
    await page.route("**/api/v1/work-items/wi-pr-block/ai-review", async (route) => {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          summary: "变更触碰核心部署流水线且存在密钥泄露风险，建议阻断合入。",
          score: 45,
          confidence: 5,
          category: "Security Fix",
          merge_risk: "High",
          maintainer_verdict: "Block Risk",
          sensitive_assets: [
            "⚙️ .github/workflows/deploy.yml (CI/CD 敏感流水线)",
            "🔑 configs/production.yaml (生产凭据配置)",
          ],
          security_risks: ["检测到潜在明文密钥泄漏", "缺少跨站请求伪造 (CSRF) 防护"],
          breaking_risks: ["移除遗留公开认证接口 authenticateV1"],
          code_smells: ["函数超过 200 行未拆分"],
          missing_tests: ["缺少密钥轮换失败场景回归测试"],
          suggestions: [
            {
              title: "改用环境变量安全读取密钥",
              file_path: "configs/production.yaml",
              description: "避免在配置文件中以明文硬编码任何 Token",
              suggested_code: "export APP_SECRET=${SECRET_TOKEN}",
            },
          ],
          reviewed_at: new Date().toISOString(),
          commented_on_pr: false,
          diff_truncated: false,
          head_sha: "commit-sha-101",
        }),
      });
    });

    // Mock PR #102 审查接口异常
    await page.route("**/api/v1/work-items/wi-pr-err/ai-review", async (route) => {
      await route.fulfill({
        status: 500,
        contentType: "application/json",
        body: JSON.stringify({
          error_code: "ai_service_error",
          message: "AI 审查分析服务暂时不可用，请稍后重试。",
        }),
      });
    });

    // 访问 PR 列表
    await page.goto("/pull-requests");

    // 验证 PR 列表加载成功
    await expect(page.getByText("refactor: update authentication & deploy configs")).toBeVisible({ timeout: 15_000 });
    await expect(page.getByText("feat: add third-party payment integration")).toBeVisible();

    // 1. 验证正常审查报告交互
    const prCard = page.getByRole("listitem").filter({ hasText: "#101" });
    const toggleButton = prCard.getByRole("button", { name: /AI 代码审查报告/ });
    await expect(toggleButton).toBeVisible();

    // 点击展开报告面板
    await toggleButton.click();

    // 关键断言：健康评分
    await expect(prCard.getByText("45 / 100")).toBeVisible();

    // 关键断言：维护者合并裁决徽章（最新业务改动）
    await expect(prCard.getByText("裁决: Block Risk")).toBeVisible();

    // 关键断言：关键敏感资产变动嗅探预警（最新业务改动）
    await expect(prCard.getByText(/🚨 哨兵关键资产变动预警:/)).toBeVisible();
    await expect(prCard.getByText("⚙️ .github/workflows/deploy.yml (CI/CD 敏感流水线)")).toBeVisible();
    await expect(prCard.getByText("🔑 configs/production.yaml (生产凭据配置)")).toBeVisible();

    // 关键断言：安全风险清单与缺失测试
    await expect(prCard.getByText(/🛡️ 安全风险:/)).toBeVisible();
    await expect(prCard.getByText("检测到潜在明文密钥泄漏")).toBeVisible();
    await expect(prCard.getByText(/缺失测试用例与回归风险:/)).toBeVisible();
    await expect(prCard.getByText("缺少密钥轮换失败场景回归测试")).toBeVisible();

    // 验证报告复制按钮
    const copyButton = prCard.getByRole("button", { name: /复制报告/ });
    await expect(copyButton).toBeVisible();
    await copyButton.click();
    await expect(prCard.getByRole("button", { name: /已复制/ })).toBeVisible();

    // 2. 验证异常路径：AI 审查服务失败提示与重新审查按钮
    const prErrCard = page.getByRole("listitem").filter({ hasText: "#102" });
    const toggleErrButton = prErrCard.getByRole("button", { name: /AI 代码审查报告/ });
    await toggleErrButton.click();

    // 错误信息与重试入口可见
    await expect(prErrCard.getByText(/审查结果加载失败，请稍后重试。/)).toBeVisible();
    await expect(prErrCard.getByRole("button", { name: /🔄 重新审查/ })).toBeVisible();
  });
});

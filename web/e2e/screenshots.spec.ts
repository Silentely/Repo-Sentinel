import { test } from "@playwright/test";
import path from "node:path";
import { ensureAuthenticated } from "./helpers";

test.skip(({ isMobile }) => isMobile, "截图仅在桌面端生成");

// 文档截图是发布素材而非回归断言：默认跳过，普通 e2e 运行不得改写仓库内的跟踪文件
// （浏览器重新编码会带来字节级差异，令工作区出现"怎么会变了"的噪声）。
// 需要重做 docs/public/images 下的界面截图时显式执行：
//   REPOSENTINEL_UPDATE_DOC_SCREENSHOTS=1 pnpm e2e
test.skip(
  () => process.env.REPOSENTINEL_UPDATE_DOC_SCREENSHOTS !== "1",
  "仅在显式设置 REPOSENTINEL_UPDATE_DOC_SCREENSHOTS=1 时更新文档截图",
);

test("生成文档界面截图 (Retro Neo-Brutalism Screenshots)", async ({ page }) => {
  await page.setViewportSize({ width: 1280, height: 860 });

  // 1. 登录页截图
  await page.goto("/login");
  await page.waitForTimeout(500);
  const docsImgDir = path.resolve(process.cwd(), "../docs/public/images");
  await page.screenshot({
    path: path.join(docsImgDir, "login-preview.png"),
    fullPage: false,
  });

  // 2. 登录并捕获仪表盘
  await ensureAuthenticated(page);
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(600);
  await page.screenshot({
    path: path.join(docsImgDir, "dashboard-preview.png"),
    fullPage: false,
  });

  // 3. 仓库监控列表
  await page.goto("/repos");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "repos-preview.png"),
    fullPage: false,
  });

  // 4. GitHub 配置与外部仓
  await page.goto("/github");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "github-preview.png"),
    fullPage: false,
  });

  // 5. 系统设置与智能值守
  await page.goto("/settings");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "settings-preview.png"),
    fullPage: false,
  });

  // 6. Pull Requests 与 AI 代码审查详情
  await page.route("**/api/v1/work-items?*", async (route) => {
    const url = new URL(route.request().url());
    if (url.searchParams.get("kind") === "pull_request") {
      await route.fulfill({
        status: 200,
        contentType: "application/json",
        body: JSON.stringify({
          items: [
            {
              id: "wi-pr-101",
              kind: "pull_request",
              number: 101,
              title: "feat(auth): add hardware security key & WebAuthn support",
              state: "open",
              html_url: "https://github.com/Silentely/Repo-Sentinel/pull/101",
              author: "alex-developer",
              source_updated_at: new Date().toISOString(),
              review_state: "completed",
              check_status: "passed",
              checks_total: 6,
              checks_passed: 6,
            },
            {
              id: "wi-pr-102",
              kind: "pull_request",
              number: 102,
              title: "fix(engine): optimize event deduplication & fingerprint caching",
              state: "open",
              html_url: "https://github.com/Silentely/Repo-Sentinel/pull/102",
              author: "senior-maintainer",
              source_updated_at: new Date().toISOString(),
              review_state: "completed",
              check_status: "passed",
              checks_total: 4,
              checks_passed: 4,
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

  await page.route("**/api/v1/work-items/wi-pr-101/ai-review", async (route) => {
    await route.fulfill({
      status: 200,
      contentType: "application/json",
      body: JSON.stringify({
        summary: "引入 WebAuthn 硬件密钥认证扩展。代码结构健全，已包含完整的错误处理与时序攻击防护，建议合入。",
        score: 92,
        confidence: 5,
        category: "Security Enhancement",
        merge_risk: "Low",
        maintainer_verdict: "Ready to Merge",
        sensitive_assets: [
          "🔑 configs/auth.yaml (身份验证密钥策略配置)",
        ],
        security_risks: [],
        breaking_risks: [],
        code_smells: [],
        missing_tests: [],
        suggestions: [
          {
            title: "补充超时与用户取消测试用例",
            file_path: "web/src/features/auth/webauthn.ts",
            description: "建议模拟浏览器取消硬件触控时的优雅回退体验。",
          },
        ],
        reviewed_at: new Date().toISOString(),
        commented_on_pr: false,
        diff_truncated: false,
        head_sha: "commit-sha-101",
      }),
    });
  });

  await page.goto("/pull-requests");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(600);
  // 展开第一条 PR 的 AI 审查报告
  const aiReportBtn = page.getByRole("button", { name: /AI 代码审查报告/ }).first();
  if (await aiReportBtn.isVisible()) {
    await aiReportBtn.click();
    await page.waitForTimeout(600);
  }
  await page.screenshot({
    path: path.join(docsImgDir, "pull-requests-ai-preview.png"),
    fullPage: false,
  });

  // 7. 通知渠道配置中心
  await page.goto("/notifications");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "notifications-preview.png"),
    fullPage: false,
  });

  // 8. 通知投递记录 (Outbox) 与实时监控
  await page.goto("/notifications/outbox");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "outbox-preview.png"),
    fullPage: false,
  });

  // 9. Webhook 检查与实时回放 (Inspector)
  await page.goto("/webhooks");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "webhooks-preview.png"),
    fullPage: false,
  });

  // 10. Star Release 追踪管理
  await page.goto("/starred-releases");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "starred-releases-preview.png"),
    fullPage: false,
  });

  // 11. 移动端视图 (Mobile Drawer & Neo-Brutalism UI)
  await page.setViewportSize({ width: 390, height: 844 });
  await page.goto("/");
  await page.waitForLoadState("networkidle");
  await page.waitForTimeout(500);
  await page.screenshot({
    path: path.join(docsImgDir, "mobile-dashboard-preview.png"),
    fullPage: false,
  });
});

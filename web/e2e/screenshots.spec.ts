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
});

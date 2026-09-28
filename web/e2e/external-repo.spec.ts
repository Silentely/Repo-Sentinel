import { expect, test } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

/**
 * 「登记外部公开仓库」的两种结果在真实浏览器里的提示语：
 * 新建（201）承诺进入基线同步；已登记（200 幂等）必须说明状态未变动。
 * 两个 project 共用同一实例与数据库，故按 project 名区分仓库名。
 */
test("添加外部仓：新建与已登记的提示语区分", async ({ page }, testInfo) => {
  await ensureAuthenticated(page);
  const repoName = `octocat/Hello-World-${testInfo.project.name}`;

  await page.goto("/github");
  const input = page.getByPlaceholder("owner/repo");
  await expect(input).toBeVisible({ timeout: 15_000 });

  // 首次：真实 POST 落库，提示应承诺基线同步。
  await input.fill(repoName);
  await page.getByRole("button", { name: "添加外部仓" }).click();
  await expect(page.getByText("外部公开仓库已登记，将进入基线同步。")).toBeVisible({ timeout: 15_000 });

  // 第二次：服务端幂等返回既有行，提示不得再承诺基线同步。
  await input.fill(repoName);
  await page.getByRole("button", { name: "添加外部仓" }).click();
  await expect(page.getByText("该仓库已在外部公开仓库列表中，同步状态未变动。")).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText("外部公开仓库已登记，将进入基线同步。")).toHaveCount(0);

  // 列表页确实只登记了一条（幂等分支不新建行）。
  await page.goto("/repos");
  await expect(page.getByText(repoName).first()).toBeVisible({ timeout: 15_000 });
  await expect(page.getByText(repoName)).toHaveCount(1);
});

import { expect, test, type Page } from "@playwright/test";
import fs from "node:fs";
import path from "node:path";

/**
 * 实例为单管理员模型：每个 e2e 服务只允许创建一个管理员。
 * 各用例文件统一经由守卫入口复用同一组凭据，谁先执行谁负责完成首次设置。
 */
export const adminUsername = "Repo Admin";
export const adminPassword = "安全管理员密码一二三四五六";

/**
 * 进入应用：按当前实例状态完成首次设置或登录，最终停在仪表盘。
 *
 * 会话复用：登录成功态按项目持久化（.test-run-data/auth-<project>.json），
 * 后续用例直接恢复 Cookie——登录限流为 5 次突发 + 每 12s 补 1，
 * 每条用例各登一次会确定性触发 429（反代/同 IP 共享限流桶）。
 */
export async function ensureAuthenticated(page: Page) {
  const stateFile = path.join("../.test-run-data", `auth-${test.info().project.name}.json`);
  if (restoreSession(page, stateFile)) {
    await page.goto("/");
    // Cookie 仍有效则直达仪表盘；失效（重启清库/会话过期）则回退完整登录流程。
    if (!page.url().endsWith("/login")) {
      await expect(page).toHaveURL(/\/$/);
      return;
    }
  }

  await page.goto("/");

  // 未初始化的实例会经过 "/" → /login → /setup 跳转链，已初始化则停在 /login。
  // 轮询直到页面内容与 URL 一致，避免在跳转链中间态上误填表单。
  const setupHeading = page.getByRole("heading", { name: "创建唯一管理员" });
  const loginButton = page.getByRole("button", { name: "登录" });
  await expect(async () => {
    if (await setupHeading.isVisible()) {
      expect(page.url()).toMatch(/\/setup$/);
    } else {
      expect(await loginButton.isVisible()).toBe(true);
      expect(page.url()).toMatch(/\/login$/);
    }
  }).toPass({ timeout: 15_000 });

  if (page.url().endsWith("/setup")) {
    await page.getByRole("textbox", { name: "用户名" }).fill(adminUsername);
    await page.getByLabel("密码", { exact: true }).fill(adminPassword);
    await page.getByLabel("确认密码").fill(adminPassword);
    await page.getByRole("button", { name: "创建管理员" }).click();
    // 冷启动（双实例+懒加载 chunk）下登录后跳转可能超过默认 5s，放宽避免时序性 flaky。
    await expect(page).toHaveURL(/\/$/, { timeout: 15_000 });
  } else {
    await page.getByRole("textbox", { name: "用户名" }).fill(adminUsername);
    await page.getByLabel("密码", { exact: true }).fill(adminPassword);
    // 冷启动下首次提交可能慢到超过单次断言窗口：以 toPass 重试整个提交动作
    //（重复提交登录幂等，会话覆盖写），彻底消除时序性 flaky。
    await expect(async () => {
      await loginButton.click();
      await expect(page).toHaveURL(/\/$/, { timeout: 5_000 });
    }).toPass({ timeout: 30_000 });
  }

  // 持久化会话供后续用例复用（见函数头注释的限流约束）。
  fs.mkdirSync(path.dirname(stateFile), { recursive: true });
  await page.context().storageState({ path: stateFile });
}

/** 从持久化文件恢复会话 Cookie 到当前上下文；文件不存在或无效返回 false。 */
function restoreSession(page: Page, stateFile: string): boolean {
  type CookieParam = Parameters<ReturnType<Page["context"]>["addCookies"]>[0];
  if (!fs.existsSync(stateFile)) {
    return false;
  }
  try {
    const state = JSON.parse(fs.readFileSync(stateFile, "utf8")) as { cookies?: CookieParam };
    if (!Array.isArray(state.cookies) || state.cookies.length === 0) {
      return false;
    }
    void page.context().addCookies(state.cookies);
    return true;
  } catch {
    return false;
  }
}

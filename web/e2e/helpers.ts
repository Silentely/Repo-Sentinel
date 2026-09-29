import { expect, test, type Page } from "@playwright/test";
import crypto from "node:crypto";
import fs from "node:fs";
import path from "node:path";

/**
 * 实例为单管理员模型：每个 e2e 服务只允许创建一个管理员。
 * 各用例文件统一经由守卫入口复用同一组凭据，谁先执行谁负责完成首次设置。
 */
export const adminUsername = "Repo Admin";
export const adminPassword = "安全管理员密码一二三四五六";

/**
 * 解码 Base32 编码字符串为 Buffer
 */
function base32Decode(base32: string): Buffer {
  const alphabet = "ABCDEFGHIJKLMNOPQRSTUVWXYZ234567";
  const cleaned = base32.toUpperCase().replace(/=+$/, "").replace(/\s+/g, "");
  let bits = 0;
  let value = 0;
  const bytes: number[] = [];

  for (let i = 0; i < cleaned.length; i++) {
    const idx = alphabet.indexOf(cleaned[i]);
    if (idx === -1) continue;
    value = (value << 5) | idx;
    bits += 5;
    if (bits >= 8) {
      bytes.push((value >>> (bits - 8)) & 255);
      bits -= 8;
    }
  }
  return Buffer.from(bytes);
}

/**
 * 基于 RFC 6238 标准生成 6 位 TOTP 动态口令
 */
export function generateTOTP(secret: string, timeStep = 30): string {
  const key = base32Decode(secret);
  const epoch = Math.floor(Date.now() / 1000);
  const counter = Math.floor(epoch / timeStep);
  const buf = Buffer.alloc(8);
  buf.writeBigUInt64BE(BigInt(counter));

  const hmac = crypto.createHmac("sha1", key).update(buf).digest();
  const offset = hmac[hmac.length - 1] & 0x0f;
  const code =
    ((hmac[offset] & 0x7f) << 24) |
    ((hmac[offset + 1] & 0xff) << 16) |
    ((hmac[offset + 2] & 0xff) << 8) |
    (hmac[offset + 3] & 0xff);

  return (code % 1000000).toString().padStart(6, "0");
}

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
    // SPA 跳转链（/ → /login|/setup）异步：URL 中间态可能是 "/"，等「仪表盘标题 / 登录按钮 / 设置标题」
    // 三者其一真正可见后再判定，不能把过渡态误当有效会话。
    const dashHeading = page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" });
    const setupHeading = page.getByRole("heading", { name: "创建唯一管理员" });
    const loginButton = page.getByRole("button", { name: "登录" });
    await expect(async () => {
      expect(
        (await dashHeading.isVisible()) || (await setupHeading.isVisible()) || (await loginButton.isVisible()),
      ).toBe(true);
    }).toPass({ timeout: 15_000 });
    if (await dashHeading.isVisible()) {
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

  if (await setupHeading.isVisible()) {
    await page.getByLabel("用户名").fill(adminUsername);
    await page.getByLabel("密码", { exact: true }).fill(adminPassword);
    await page.getByLabel("确认密码").fill(adminPassword);
    await page.getByRole("button", { name: "创建管理员" }).click();
  } else {
    await page.getByLabel("用户名").fill(adminUsername);
    await page.getByLabel("密码").fill(adminPassword);
    await loginButton.click();
  }

  await expect(page.getByRole("heading", { name: "现在是否健康，今天发生了什么。" })).toBeVisible({
    timeout: 15_000,
  });

  const cookies = await page.context().cookies();
  fs.mkdirSync(path.dirname(stateFile), { recursive: true });
  fs.writeFileSync(stateFile, JSON.stringify({ cookies }, null, 2));
}

type CookieParam = Parameters<Page["context"]["addCookies"]>[0];

function restoreSession(page: Page, stateFile: string): boolean {
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

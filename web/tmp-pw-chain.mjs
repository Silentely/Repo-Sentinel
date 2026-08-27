// 复现 auth.spec 前段：goto("/") → 应跳 /setup
import { chromium } from "@playwright/test";

const base = process.env.BASE_URL || "http://127.0.0.1:34117";
const browser = await chromium.launch();
const page = await browser.newPage();
page.on("console", (m) => { if (m.type() === "error") console.log("[console.error]", m.text().slice(0, 200)); });
page.on("pageerror", (e) => console.log("[pageerror]", String(e).slice(0, 300)));
page.on("response", (r) => { if (r.status() >= 400) console.log("[resp]", r.status(), r.url()); });

await page.goto(base + "/");
for (let i = 0; i < 16; i++) {
  await page.waitForTimeout(500);
  console.log(`t=${(i + 1) * 0.5}s url=${page.url()} heading=${await page.getByRole("heading").first().textContent().catch(() => "?")}`);
}
await browser.close();

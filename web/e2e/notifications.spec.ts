import { expect, test, type Page } from "@playwright/test";

import { ensureAuthenticated } from "./helpers";

async function cleanupChannels(page: Page) {
  await page.evaluate(async () => {
    const csrf = (document.cookie.match(/(?:^|; )reposentinel_csrf=([^;]+)/) || [])[1] || "";
    const headers = { "Content-Type": "application/json", "X-CSRF-Token": decodeURIComponent(csrf) };
    for (const type of ["http_webhook", "telegram", "feishu", "wecom", "dingtalk", "discord", "bark"]) {
      await fetch(`/api/v1/notifications/channels/${type}`, {
        method: "DELETE",
        credentials: "include",
        headers,
      }).catch(() => undefined);
    }
  });
}

test.describe("通知渠道配置与交互", () => {
  test.afterEach(async ({ page }) => {
    await cleanupChannels(page);
  });

  test("渠道表单校验：非空拦截、URL 协议要求与 Telegram 格式反馈", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/notifications");

    const webhookSection = page.locator("section.channel-form").filter({ hasText: "HTTP Webhook" });
    const webhookInput = webhookSection.getByLabel("HTTPS URL");
    await expect(webhookInput).toBeVisible({ timeout: 15_000 });

    // 1. 空 URL 提交拦截
    await webhookInput.fill("");
    await webhookSection.getByRole("button", { name: "保存 HTTP Webhook" }).click();
    await expect(page.getByText("请填写 HTTPS URL。")).toBeVisible();

    // 2. 非 HTTPS 协议即时失焦提示
    await webhookInput.fill("http://insecure-endpoint.com/hook");
    await webhookInput.blur();
    await expect(webhookSection.getByRole("status").filter({ hasText: "出站投递仅允许 HTTPS URL。" })).toBeVisible();

    // 3. 非法 URL 格式失焦提示
    await webhookInput.fill("not-a-valid-url");
    await webhookInput.blur();
    await expect(webhookSection.getByRole("status").filter({ hasText: "URL 格式无法解析，请检查是否完整。" })).toBeVisible();

    // 4. Telegram Chat ID 格式校验（非数字提示）
    const telegramSection = page.locator("section.channel-form").filter({ hasText: "Telegram" });
    const telegramInput = telegramSection.getByLabel("Chat ID");
    await telegramInput.fill("not-a-chat-id");
    await telegramInput.blur();
    await expect(telegramSection.getByRole("status").filter({ hasText: "Chat ID 应为数字；群组通常以 -100 开头。" })).toBeVisible();
  });

  test("HTTP Webhook 渠道创建、刷新回读、删除二次确认取消与清理", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/notifications");

    const webhookSection = page.locator("section.channel-form").filter({ hasText: "HTTP Webhook" });
    const webhookInput = webhookSection.getByLabel("HTTPS URL");
    const secretInput = webhookSection.getByLabel("签名 Secret（可选）");
    await expect(webhookInput).toBeVisible({ timeout: 15_000 });

    const targetUrl = "https://example.com/e2e-alert-sink";
    await webhookInput.fill(targetUrl);
    await secretInput.fill("super-secure-e2e-secret");

    // 保存渠道
    await webhookSection.getByRole("button", { name: "保存 HTTP Webhook" }).click();
    await expect(page.getByRole("status").filter({ hasText: "HTTP Webhook 渠道已保存。" })).toBeVisible();

    // 在当前渠道列表中断言配置已生效
    const currentSection = page.locator("section.onboarding-card").filter({ hasText: "当前渠道" });
    const channelRow = currentSection.locator(".channel-row").filter({ hasText: "HTTP Webhook" });
    await expect(channelRow).toBeVisible();
    await expect(channelRow).toContainText("已启用");
    await expect(channelRow).toContainText(targetUrl);
    await expect(channelRow).toContainText("密钥已配置");

    // 页面刷新后回读一致（持久化验证）
    await page.reload();
    await expect(channelRow).toBeVisible({ timeout: 15_000 });
    await expect(channelRow).toContainText("已启用");
    await expect(channelRow).toContainText(targetUrl);

    // 删除二次确认：点击取消不触发删除
    await channelRow.getByRole("button", { name: "删除" }).click();
    const dialog = page.getByRole("dialog", { name: "删除渠道" });
    await expect(dialog).toBeVisible();
    await expect(dialog).toContainText("确定要删除 HTTP Webhook 渠道吗？");
    await dialog.locator(".secondary-button").click();
    await expect(dialog).toBeHidden();
    await expect(channelRow).toBeVisible();

    // 再次点击删除并确认
    await channelRow.getByRole("button", { name: "删除" }).click();
    await expect(dialog).toBeVisible();
    await dialog.locator(".primary-button--danger").click();
    await expect(dialog).toBeHidden();
    await expect(page.getByRole("status").filter({ hasText: "HTTP Webhook 渠道已删除。" })).toBeVisible();

    // 确认列表中该渠道已被移除
    await expect(channelRow).toHaveCount(0);
  });

  test("多渠道协同配置：企业微信、飞书、钉钉、Discord 及 Bark 的表单保存与状态呈现", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/notifications");
    const currentSection = page.locator("section.onboarding-card").filter({ hasText: "当前渠道" });

    // 1. 飞书 / Lark 配置与生效断言
    const feishuSection = page.locator("section.channel-form").filter({ hasText: "飞书 / Lark" });
    await feishuSection.getByLabel("Webhook 地址").fill("https://open.feishu.cn/open-apis/bot/v2/hook/e2e-feishu-token");
    await feishuSection.getByLabel("加签密钥（可选，留空保留原密钥）").fill("feishu-sign-key");
    await feishuSection.getByRole("button", { name: "保存 飞书 / Lark" }).click();
    await expect(currentSection.locator(".channel-row").filter({ hasText: "飞书 / Lark" })).toBeVisible();

    // 2. 钉钉配置与生效断言
    const dingtalkSection = page.locator("section.channel-form").filter({ hasText: "钉钉" });
    await dingtalkSection.getByLabel("Webhook 地址").fill("https://oapi.dingtalk.com/robot/send?access_token=e2e-token");
    await dingtalkSection.getByLabel("加签密钥（可选，留空保留原密钥）").fill("SEC-e2e-secret");
    await dingtalkSection.getByRole("button", { name: "保存 钉钉" }).click();
    await expect(currentSection.locator(".channel-row").filter({ hasText: "钉钉" })).toBeVisible();

    // 3. 企业微信配置与生效断言
    const wecomSection = page.locator("section.channel-form").filter({ hasText: "企业微信" });
    await wecomSection.getByLabel("Webhook 地址").fill("https://qyapi.weixin.qq.com/cgi-bin/webhook/send?key=e2e-wecom-key");
    await wecomSection.getByRole("button", { name: "保存 企业微信" }).click();
    await expect(currentSection.locator(".channel-row").filter({ hasText: "企业微信" })).toBeVisible();

    // 4. Discord 配置与生效断言
    const discordSection = page.locator("section.channel-form").filter({ hasText: "Discord" });
    await discordSection.getByLabel("Webhook 地址").fill("https://discord.com/api/webhooks/123456/abcdef");
    await discordSection.getByRole("button", { name: "保存 Discord" }).click();
    await expect(currentSection.locator(".channel-row").filter({ hasText: "Discord" })).toBeVisible();

    // 5. Bark (iOS) 配置与生效断言
    const barkSection = page.locator("section.channel-form").filter({ hasText: "Bark (iOS)" });
    await barkSection.getByLabel("设备 Key 或服务器 URL").fill("https://api.day.app/e2e-device-key");
    await barkSection.getByRole("button", { name: "保存 Bark (iOS)" }).click();
    await expect(currentSection.locator(".channel-row").filter({ hasText: "Bark (iOS)" })).toBeVisible();

    // 综合断言：列表中所有 5 个渠道均处于「已启用」状态
    for (const name of ["飞书 / Lark", "钉钉", "企业微信", "Discord", "Bark (iOS)"]) {
      const row = currentSection.locator(".channel-row").filter({ hasText: name });
      await expect(row).toBeVisible();
      await expect(row).toContainText("已启用");
    }
  });
});

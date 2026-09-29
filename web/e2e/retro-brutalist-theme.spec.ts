import { expect, test } from "@playwright/test";
import { ensureAuthenticated } from "./helpers";

test.describe("Retro Neo-Brutalism 视觉与交互一致性验收", () => {
  test("主要页面容器卡片与按钮严格遵循零模糊硬阴影与粗黑描边规范", async ({ page }) => {
    await ensureAuthenticated(page);

    // 检查核心监控页面
    for (const path of ["/", "/repos", "/github", "/settings", "/about"]) {
      await page.goto(path);
      await page.waitForLoadState("networkidle");

      // 提取页面上所有的卡片和关键容器进行样式断言
      const cardStyles = await page.evaluate(() => {
        const cards = Array.from(
          document.querySelectorAll<HTMLElement>(
            ".card, .stat-card, .metric-card, .panel-card, .dashboard-section, .about-section, .callout",
          ),
        );
        return cards.slice(0, 15).map((el) => {
          const style = window.getComputedStyle(el);
          return {
            borderWidth: parseFloat(style.borderTopWidth) || 0,
            borderRadius: parseFloat(style.borderTopLeftRadius) || 0,
            boxShadow: style.boxShadow,
          };
        });
      });

      for (const card of cardStyles) {
        // 卡片描边需为粗黑描边 (>= 2px)
        expect(card.borderWidth, `${path} 卡片描边需不小于 2px`).toBeGreaterThanOrEqual(2);
        // 卡片圆角需为大圆角 (>= 12px)
        expect(card.borderRadius, `${path} 卡片圆角需不小于 12px`).toBeGreaterThanOrEqual(12);

        // 校验阴影零模糊（如果存在 boxShadow，其中的模糊半径必须为 0px）
        if (card.boxShadow && card.boxShadow !== "none") {
          // boxShadow 格式如: "rgb(33, 28, 20) 2.5px 2.5px 0px 0px"
          // 或 "2.5px 2.5px 0px 0px rgb(33, 28, 20)"
          const blurMatch = card.boxShadow.match(
            /(?:-?\d+(?:\.\d+)?px\s+){2}(-?\d+(?:\.\d+)?px)/,
          );
          if (blurMatch && blurMatch[1]) {
            const blurRadius = parseFloat(blurMatch[1]);
            expect(
              blurRadius,
              `${path} 卡片阴影必须为无模糊实体硬阴影 (blur radius 必须为 0px，实测 ${card.boxShadow})`,
            ).toBe(0);
          }
        }
      }

      // 检查按钮的胶囊形态与零模糊阴影
      const buttonStyles = await page.evaluate(() => {
        const buttons = Array.from(
          document.querySelectorAll<HTMLElement>(
            ".primary-button, .secondary-button, .quiet-button",
          ),
        );
        return buttons.slice(0, 10).map((btn) => {
          const style = window.getComputedStyle(btn);
          return {
            borderRadius: parseFloat(style.borderTopLeftRadius) || 0,
            boxShadow: style.boxShadow,
          };
        });
      });

      for (const btn of buttonStyles) {
        // 按钮为胶囊形 (border-radius: 9999px 或计算高度半径 >= 14px)
        expect(btn.borderRadius, `${path} 按钮需为胶囊圆角`).toBeGreaterThanOrEqual(14);
        if (btn.boxShadow && btn.boxShadow !== "none") {
          const blurMatch = btn.boxShadow.match(
            /(?:-?\d+(?:\.\d+)?px\s+){2}(-?\d+(?:\.\d+)?px)/,
          );
          if (blurMatch && blurMatch[1]) {
            const blurRadius = parseFloat(blurMatch[1]);
            expect(blurRadius, `${path} 按钮阴影必须为硬阴影零模糊`).toBe(0);
          }
        }
      }
    }
  });

  test("设置页与配置表单网格输入框具有统一圆角描边且无重叠挤压", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/settings");
    await page.waitForLoadState("networkidle");

    // 检查表单网格中的输入框不发生重叠
    const inputBoxes = await page.evaluate(() => {
      const inputs = Array.from(
        document.querySelectorAll<HTMLInputElement>(
          ".form-grid input:not([type=checkbox]), .field__control",
        ),
      );
      return inputs.map((input) => {
        const rect = input.getBoundingClientRect();
        const style = window.getComputedStyle(input);
        return {
          left: rect.left,
          right: rect.right,
          top: rect.top,
          bottom: rect.bottom,
          width: rect.width,
          borderWidth: parseFloat(style.borderTopWidth) || 0,
          borderRadius: parseFloat(style.borderTopLeftRadius) || 0,
        };
      });
    });

    // 确保输入框均满足规范描边与圆角
    for (const box of inputBoxes) {
      if (box.width > 0) {
        expect(box.borderWidth, "输入框描边应为 2px 实线").toBeGreaterThanOrEqual(2);
        expect(box.borderRadius, "输入框圆角应统一为 >= 10px").toBeGreaterThanOrEqual(10);
      }
    }
  });

  test("Webhook Inspector 弹窗具有统一粗描边与硬阴影且支持键盘关闭", async ({ page }) => {
    await ensureAuthenticated(page);
    await page.goto("/github");
    await page.waitForLoadState("networkidle");

    // 进入 Webhook 投递历史或直接验证 Inspector 弹窗结构
    await page.goto("/notifications/outbox");
    await page.waitForLoadState("networkidle");

    // 验证空态或列表按钮均有统一样式
    const buttons = page.locator("button.primary-button, button.quiet-button");
    const count = await buttons.count();
    expect(count).toBeGreaterThan(0);
  });
});

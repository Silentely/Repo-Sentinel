package digest

import (
	"strings"
	"testing"

	"github.com/Silentely/Repo-Sentinel/internal/store"
)

func TestFormatDigestSummaryHTML_UserScenario(t *testing.T) {
	input := `过去 24 小时共 60 条事件，以 Renovate 依赖更新和仪表盘刷新为主，另有 1 条高危安全告警、4 个版本发布和 1 个 Bug 问题。

- 安全告警：Silentely/xserver-vps-renew #21 对 source-map-js 标记严重度 high，需尽快确认（同仓库 #20 为其 1.2.1→1.2.2 升级）。
- 版本发布：samanhappy/mcphub v1.1.3、spacering-net/codeg v0.35.1 与 v0.35.0、ardeyouxipianyi/workbuddy2api-hub v1.6.20。
- 依赖更新集中于 Silentely/xserver-vps-renew：#23 actions/checkout v7、#22 eslint v9.39.5、#17 nanoid v3.3.20、#21 golang Docker v1.27、#19 @evomap/evolver v1.94.0、#16 @puppeteer/browsers v3.2.4，另有 #18 Dependency Dashboard。
- Silentely/actor-monitor：#60 升级 python 至 3.15，#4 Dependency Dashboard。
- Renovate 配置：Silentely/Repo-Sentinel #55 与 Silentely/telegram-private-chat-gateway #3 均新增 Configure Renovate。
- 问题跟踪：Silentely/TG-SignPulse #13 [Bug] 执行任务时收到某些回复后停止并误标为完成。
- 本时段未见 CI 失败或已合并 PR 的记录。`

	output := formatDigestSummaryHTML(input)

	// 1. 验证常规依赖更新进入了 Telegram 原生可折叠块
	if !strings.Contains(output, "<blockquote expandable>") || !strings.Contains(output, "</blockquote>") {
		t.Fatalf("依赖更新应放入 Telegram <blockquote expandable> 折叠块，实际:\n%s", output)
	}
	if !strings.Contains(output, "📦 常规依赖更新与次要动态（点击展开）") {
		t.Fatalf("应包含折叠块标题，实际:\n%s", output)
	}

	// 2. 验证重点告警置顶且加粗
	if !strings.Contains(output, "🚨 <b>安全告警：</b>") {
		t.Fatalf("安全告警应加粗突出，实际:\n%s", output)
	}
	if !strings.Contains(output, "<b>HIGH</b>") {
		t.Fatalf("严重度 high 应加粗突出，实际:\n%s", output)
	}

	// 3. 验证仓库名和 issue 编号等宽/加粗
	if !strings.Contains(output, "<code>Silentely/xserver-vps-renew</code>") {
		t.Fatalf("仓库名应使用 <code> 等宽标签，实际:\n%s", output)
	}
	if !strings.Contains(output, "<b>#21</b>") {
		t.Fatalf("编号应使用 <b> 加粗，实际:\n%s", output)
	}

	// 验证版本号迁移格式化
	if !strings.Contains(output, "<code>1.2.1</code> ➔ <code>1.2.2</code>") {
		t.Fatalf("版本迁移应高亮为 <code>1.2.1</code> ➔ <code>1.2.2</code>，实际:\n%s", output)
	}

	// 验证紧急行动项高亮
	if !strings.Contains(output, "<b><u>需尽快确认</u></b>") {
		t.Fatalf("需尽快确认应带有 <b><u> 强调标签，实际:\n%s", output)
	}

	// 4. 验证版本发布
	if !strings.Contains(output, "🚀 <b>版本发布：</b>") {
		t.Fatalf("版本发布应加粗突出，实际:\n%s", output)
	}
	if !strings.Contains(output, "<code>v1.1.3</code>") {
		t.Fatalf("版本号应使用 <code> 标签，实际:\n%s", output)
	}

	// 5. 验证问题跟踪
	if !strings.Contains(output, "🐛 <b>问题跟踪：</b>") {
		t.Fatalf("问题跟踪应使用 🐛 图标并加粗，实际:\n%s", output)
	}
}

func TestBuildDigestMetricsCard(t *testing.T) {
	events := []store.Event{
		{Kind: store.AlertKindDependabot, Severity: "high", Title: "dependabot alert"},
		{Kind: store.WorkflowRunKind, Action: "failure", Title: "CI"},
		{Kind: store.ReleaseKind, Title: "v1.0.0"},
		{Kind: store.WorkItemKindIssue, Title: "bug report"},
		{Kind: store.WorkItemKindPR, Title: "Update actions/checkout action to v4"},
	}

	card := buildDigestMetricsCard(events, "过去 24 小时")

	if !strings.Contains(card, "🚨 <b>[需紧急介入]</b>") {
		t.Fatalf("含有高危告警时应显示紧急介入态势徽标，实际:\n%s", card)
	}
	if !strings.Contains(card, "过去 24 小时 · 共 5 条事件") {
		t.Fatalf("指标卡应包含统计时段和总数，实际:\n%s", card)
	}
	if !strings.Contains(card, "🚨 高危告警: 1") {
		t.Fatalf("应包含高危告警数，实际:\n%s", card)
	}
	if !strings.Contains(card, "❌ CI失败: 1") {
		t.Fatalf("应包含 CI 失败数，实际:\n%s", card)
	}
	if !strings.Contains(card, "🐛 缺陷: 1") {
		t.Fatalf("应包含缺陷数，实际:\n%s", card)
	}
	if !strings.Contains(card, "🚀 发布: 1") {
		t.Fatalf("应包含发布数，实际:\n%s", card)
	}
}

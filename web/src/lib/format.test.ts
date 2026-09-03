import { describe, expect, it } from "vitest";

import {
  repoDisplayName,
  syncStatusLabel,
  outboxStatusLabel,
  alertStateLabel,
  eventActionLabel,
  eventKindLabel,
  formatRelativeTime,
  htmlToPlainText,
  outboxErrorHint,
  severityLabel,
  workItemStateLabel,
  workflowConclusionLabel,
} from "./format";

describe("formatRelativeTime", () => {
  const now = new Date("2026-08-07T12:00:00Z");

  it("空串与非法日期返回空串", () => {
    expect(formatRelativeTime("")).toBe("");
    expect(formatRelativeTime("not-a-date")).toBe("");
    expect(formatRelativeTime("2026-08-07T12:00:00Z", new Date("invalid"))).toBe("");
  });

  it("60 秒内与未来时间（时钟偏差）归为刚刚，不渲染空白", () => {
    expect(formatRelativeTime("2026-08-07T11:59:30Z", now)).toBe("刚刚");
    expect(formatRelativeTime("2026-08-07T12:00:01Z", now)).toBe("刚刚");
  });

  it("分钟/小时/天粒度", () => {
    expect(formatRelativeTime("2026-08-07T11:55:00Z", now)).toBe("5 分钟前");
    expect(formatRelativeTime("2026-08-07T09:00:00Z", now)).toBe("3 小时前");
    expect(formatRelativeTime("2026-08-05T12:00:00Z", now)).toBe("2 天前");
  });

  it("超过 30 天用月/年粒度，与相对时间风格一致", () => {
    expect(formatRelativeTime("2026-06-23T12:00:00Z", now)).toBe("1 个月前");
    expect(formatRelativeTime("2025-07-03T12:00:00Z", now)).toBe("1 年前");
  });
});

describe("eventActionLabel", () => {
  it("支持 star/watch kind 感知文案", () => {
    expect(eventActionLabel("created", "star")).toBe("已收藏");
    expect(eventActionLabel("deleted", "star")).toBe("取消收藏");
    expect(eventActionLabel("started", "watch")).toBe("已关注");
  });

  it("非 star/watch kind 或未传 kind 走既有逻辑，不回归", () => {
    expect(eventActionLabel("opened")).toBe("已打开");
    expect(eventActionLabel("opened", "issue")).toBe("已打开");
    // star 的其它动作不在专用文案内时回退既有逻辑。
    expect(eventActionLabel("updated", "star")).toBe("已更新");
    expect(eventActionLabel("created", "dependabot")).toBe("新告警");
  });

  it("告警 withdrawn 动作中文化", () => {
    expect(eventActionLabel("withdrawn", "dependabot")).toBe("已撤回");
    expect(eventActionLabel("withdrawn", "code_scanning")).toBe("已撤回");
    expect(eventActionLabel("withdrawn")).toBe("已撤回");
  });
});

describe("workItemStateLabel", () => {
  it("open/closed 中文化，未知值原样回退", () => {
    expect(workItemStateLabel("open")).toBe("未关闭");
    expect(workItemStateLabel("closed")).toBe("已关闭");
    expect(workItemStateLabel("merged")).toBe("merged");
    expect(workItemStateLabel("")).toBe("—");
  });
});

describe("workflowConclusionLabel", () => {
  it("常见结论中文化", () => {
    expect(workflowConclusionLabel("success")).toBe("成功");
    expect(workflowConclusionLabel("failure")).toBe("失败");
    expect(workflowConclusionLabel("startup_failure")).toBe("失败");
    expect(workflowConclusionLabel("cancelled")).toBe("已取消");
    expect(workflowConclusionLabel("timed_out")).toBe("超时");
    expect(workflowConclusionLabel("skipped")).toBe("已跳过");
    expect(workflowConclusionLabel("in_progress")).toBe("进行中");
  });

  it("未知结论原样回退", () => {
    expect(workflowConclusionLabel("neutral")).toBe("neutral");
    expect(workflowConclusionLabel("")).toBe("—");
  });
});

describe("severityLabel", () => {
  it("GitHub 严重度枚举中文化", () => {
    expect(severityLabel("critical")).toBe("严重");
    expect(severityLabel("high")).toBe("高");
    expect(severityLabel("error")).toBe("高");
    expect(severityLabel("medium")).toBe("中");
    expect(severityLabel("warning")).toBe("中");
    expect(severityLabel("low")).toBe("低");
    expect(severityLabel("note")).toBe("低");
  });

  it("未知严重度原样回退", () => {
    expect(severityLabel("unknown")).toBe("unknown");
    expect(severityLabel("")).toBe("");
  });
});

describe("alertStateLabel", () => {
  it("告警状态中文化", () => {
    expect(alertStateLabel("open")).toBe("待处理");
    expect(alertStateLabel("dismissed")).toBe("GitHub 已忽略");
    expect(alertStateLabel("fixed")).toBe("已修复");
    expect(alertStateLabel("resolved")).toBe("已修复");
    expect(alertStateLabel("auto_dismissed")).toBe("自动忽略");
    expect(alertStateLabel("withdrawn")).toBe("已撤回");
  });

  it("未知状态原样回退", () => {
    expect(alertStateLabel("whatever")).toBe("whatever");
    expect(alertStateLabel("")).toBe("—");
  });
});

describe("syncStatusLabel / outboxStatusLabel 空安全", () => {
  it("undefined / null / 空串安全返回空串", () => {
    expect(syncStatusLabel(undefined)).toBe("");
    expect(syncStatusLabel(null)).toBe("");
    expect(syncStatusLabel("")).toBe("");
    expect(outboxStatusLabel(undefined)).toBe("");
    expect(outboxStatusLabel(null)).toBe("");
    expect(outboxStatusLabel("")).toBe("");
  });
});

describe("eventKindLabel", () => {
  it("安全告警 kind 复用 alertKindLabel 中文名，与安全页一致", () => {
    expect(eventKindLabel("dependabot")).toBe("依赖漏洞");
    expect(eventKindLabel("code_scanning")).toBe("代码扫描");
    expect(eventKindLabel("secret_scanning")).toBe("密钥泄露");
  });

  it("Issue/PR/Actions/Star/Watch 保留品牌词", () => {
    expect(eventKindLabel("issue")).toBe("Issue");
    expect(eventKindLabel("pull_request")).toBe("PR");
    expect(eventKindLabel("workflow_run")).toBe("Actions");
    expect(eventKindLabel("star")).toBe("Star");
    expect(eventKindLabel("watch")).toBe("Watch");
  });
});

describe("outboxErrorHint", () => {
  it("常见错误码给出中文排障提示", () => {
    expect(outboxErrorHint("telegram_rate_limited")).toContain("限流");
    expect(outboxErrorHint("telegram_client_error_400")).toContain("Chat ID");
    expect(outboxErrorHint("http_webhook_status_503")).toContain("服务端错误");
    expect(outboxErrorHint("decrypt_secret")).toContain("密钥");
    expect(outboxErrorHint("database_unavailable")).toContain("数据库");
  });

  it("未收录错误码返回空串，不占用展示空间", () => {
    expect(outboxErrorHint("some_unknown_code")).toBe("");
    expect(outboxErrorHint("")).toBe("");
  });
});

describe("htmlToPlainText", () => {
  it("剔除标签、保留链接 URL、反转义实体", () => {
    const html = `<b>标题</b>\n<code>x &lt; y</code> <a href="https://example.com/a?b=1&amp;c=2">链接</a>`;
    const got = htmlToPlainText(html);
    expect(got).not.toContain("<b>");
    expect(got).toContain("x < y");
    expect(got).toContain("链接 (https://example.com/a?b=1&c=2)");
  });
});

describe("repoDisplayName", () => {
  it("空值或缺省安全回退空串", () => {
    expect(repoDisplayName(null)).toBe("");
    expect(repoDisplayName(undefined)).toBe("");
  });

  it("支持优先使用 full_name 或 owner/name 拼接", () => {
    expect(repoDisplayName({ full_name: "acme/repo" })).toBe("acme/repo");
    expect(repoDisplayName({ owner: "acme", name: "tool" })).toBe("acme/tool");
  });
});

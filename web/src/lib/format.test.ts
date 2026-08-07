import { describe, expect, it } from "vitest";

import {
  alertStateLabel,
  eventActionLabel,
  eventKindLabel,
  severityLabel,
  workItemStateLabel,
  workflowConclusionLabel,
} from "./format";

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
  });

  it("未知状态原样回退", () => {
    expect(alertStateLabel("whatever")).toBe("whatever");
    expect(alertStateLabel("")).toBe("—");
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

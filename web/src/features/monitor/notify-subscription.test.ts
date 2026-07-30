import { describe, expect, it } from "vitest";

import { SUBSCRIBABLE_KINDS, subscriptionSummary, uiCheckedKinds } from "./notify-subscription";

describe("uiCheckedKinds", () => {
  it("null 渲染为全部勾选", () => {
    expect(uiCheckedKinds(null)).toEqual(SUBSCRIBABLE_KINDS.map((k) => k.value));
  });
  it("空数组表示不订阅实时", () => {
    expect(uiCheckedKinds([])).toEqual([]);
  });
  it("子集原样返回", () => {
    expect(uiCheckedKinds(["issue"])).toEqual(["issue"]);
  });
});

describe("subscriptionSummary", () => {
  it("null 显示全部类型并标注汇总", () => {
    expect(subscriptionSummary(null, true)).toContain("全部类型");
    expect(subscriptionSummary(null, true)).toContain("每日汇总");
  });
  it("空数组显示不接收实时通知", () => {
    expect(subscriptionSummary([], false)).toContain("不接收实时通知");
  });
  it("子集显示中文标签", () => {
    const text = subscriptionSummary(["issue", "workflow_run"], false);
    expect(text).toContain("Issue");
    expect(text).toContain("工作流");
  });
});

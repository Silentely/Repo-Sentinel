import { describe, expect, it } from "vitest";

import { SUBSCRIBABLE_KINDS, subscriptionSummary, uiCheckedKinds } from "./notify-subscription";

describe("SUBSCRIBABLE_KINDS", () => {
  it("订阅白名单包含 star/watch", () => {
    const values = SUBSCRIBABLE_KINDS.map((k) => k.value);
    expect(values).toContain("star");
    expect(values).toContain("watch");
  });
  it("star/watch 条目对应全局功能开关键", () => {
    expect(SUBSCRIBABLE_KINDS.find((k) => k.value === "star")?.featureKey).toBe("feature.stars");
    expect(SUBSCRIBABLE_KINDS.find((k) => k.value === "watch")?.featureKey).toBe("feature.watches");
  });
});

describe("uiCheckedKinds", () => {
  it("null 或 undefined 渲染为全部勾选", () => {
    expect(uiCheckedKinds(null)).toEqual(SUBSCRIBABLE_KINDS.map((k) => k.value));
    expect(uiCheckedKinds(undefined)).toEqual(SUBSCRIBABLE_KINDS.map((k) => k.value));
  });
  it("空数组表示不订阅实时", () => {
    expect(uiCheckedKinds([])).toEqual([]);
  });
  it("子集原样返回", () => {
    expect(uiCheckedKinds(["issue"])).toEqual(["issue"]);
  });
});

describe("subscriptionSummary", () => {
  it("null 或 undefined 显示全部类型并标注汇总", () => {
    expect(subscriptionSummary(null, true)).toContain("全部类型");
    expect(subscriptionSummary(null, true)).toContain("定期汇总");
    expect(subscriptionSummary(undefined, false)).toContain("全部类型");
  });
  it("空数组显示不接收实时通知", () => {
    expect(subscriptionSummary([], false)).toContain("不接收实时通知");
  });
  it("子集显示中文标签", () => {
    const text = subscriptionSummary(["issue", "workflow_run"], false);
    expect(text).toContain("Issue");
    expect(text).toContain("Actions");
  });
});

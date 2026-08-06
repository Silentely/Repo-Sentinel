import { describe, expect, it } from "vitest";

import { eventActionLabel } from "./format";

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

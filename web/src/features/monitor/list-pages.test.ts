import { describe, expect, it } from "vitest";

import { filterWorkItemsByText, maintainerActionStyle, resolveRepoId, type WorkItem } from "./list-pages";
import type { Repository } from "./api";

const item = (id: string, title: string): WorkItem => ({
  id,
  kind: "issue",
  number: Number(id.replace("item-", "")),
  title,
  state: "open",
  html_url: "",
  author: "alice",
});

const repo = (id: string, name: string, fullName: string): Repository => ({
  id,
  type: "installation",
  sync_status: "active",
  full_name: fullName,
  owner: fullName.split("/")[0] ?? "",
  name,
  is_private: false,
  is_archived: false,
  monitor_enabled: true,
  issues_enabled: true,
  pr_enabled: true,
  actions_enabled: true,
  alerts_enabled: true,
  stars_enabled: true,
  watches_enabled: true,
  html_url: `https://github.com/${fullName}`,
  updated_at: "2026-10-01T00:00:00Z",
});

describe("filterWorkItemsByText", () => {
  it("returns only rows matching the free-text query", () => {
    const result = filterWorkItemsByText([item("item-1", "unrelated"), item("item-2", "memory leak")], "memory");

    expect(result.map((entry) => entry.id)).toEqual(["item-2"]);
  });
});

describe("resolveRepoId", () => {
  const repos = [repo("repo-1", "sentinel", "acme/sentinel"), repo("repo-2", "web", "acme/web")];

  it("按 full_name、短名（忽略大小写）与仓库 ID 解析", () => {
    expect(resolveRepoId(repos, "acme/sentinel")).toBe("repo-1");
    expect(resolveRepoId(repos, "ACME/Web")).toBe("repo-2");
    expect(resolveRepoId(repos, "sentinel")).toBe("repo-1");
    expect(resolveRepoId(repos, "repo-2")).toBe("repo-2");
  });

  it("未命中或未提供引用时返回空串，调用方据此清空仓库筛选", () => {
    expect(resolveRepoId(repos, "acme/unknown")).toBe("");
    expect(resolveRepoId(repos, "acme/sentinel-extra")).toBe("");
    expect(resolveRepoId(repos, "")).toBe("");
    expect(resolveRepoId(repos, undefined)).toBe("");
    expect(resolveRepoId([], "acme/sentinel")).toBe("");
  });
});

describe("maintainerActionStyle", () => {
  it("动态映射阻断与拒绝动作到 danger 语义", () => {
    const s1 = maintainerActionStyle("拒绝合入：检测到漏洞", "Block Risk");
    expect(s1.background).toBe("var(--danger-bg)");
    expect(s1.color).toBe("var(--danger-fg)");

    const s2 = maintainerActionStyle("垃圾内容：建议直接关闭", "Invalid");
    expect(s2.background).toBe("var(--danger-bg)");
    expect(s2.color).toBe("var(--danger-fg)");
  });

  it("动态映射人工复核与补测到 warning 语义", () => {
    const s1 = maintainerActionStyle("人工复核：涉及敏感资产", "Needs Manual Review");
    expect(s1.background).toBe("var(--warning-bg)");
    expect(s1.color).toBe("var(--warning-fg)");

    const s2 = maintainerActionStyle("要求修改：建议补测", "Needs Tests");
    expect(s2.background).toBe("var(--warning-bg)");
    expect(s2.color).toBe("var(--warning-fg)");
  });

  it("动态映射放心合入与自检通过到 success 语义", () => {
    const s1 = maintainerActionStyle("放心合入：代码质量高", "Ready to Merge");
    expect(s1.background).toBe("var(--success-bg)");
    expect(s1.color).toBe("var(--success-fg)");

    const s2 = maintainerActionStyle("自检通过：改动安全规范", "Ready to Merge");
    expect(s2.background).toBe("var(--success-bg)");
    expect(s2.color).toBe("var(--success-fg)");
  });
});

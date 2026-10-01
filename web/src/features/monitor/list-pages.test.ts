import { describe, expect, it } from "vitest";

import { filterWorkItemsByText, resolveRepoId, type WorkItem } from "./list-pages";
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

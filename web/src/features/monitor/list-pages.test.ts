import { describe, expect, it } from "vitest";

import { filterWorkItemsByText, type WorkItem } from "./list-pages";

const item = (id: string, title: string): WorkItem => ({
  id,
  kind: "issue",
  number: Number(id.replace("item-", "")),
  title,
  state: "open",
  html_url: "",
  author: "alice",
});

describe("filterWorkItemsByText", () => {
  it("returns only rows matching the free-text query", () => {
    const result = filterWorkItemsByText([item("item-1", "unrelated"), item("item-2", "memory leak")], "memory");

    expect(result.map((entry) => entry.id)).toEqual(["item-2"]);
  });
});

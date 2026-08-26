import { beforeEach, describe, expect, it, vi } from "vitest";

const fixtures = vi.hoisted(() => ({
  apiRequest: vi.fn(),
}));

vi.mock("../../lib/api/client", () => ({
  apiRequest: fixtures.apiRequest,
}));

import { repositoriesQueryOptions } from "./api";

const repo = (id: string) => ({ id, full_name: `o/${id}` });

// 调用 queryFn 的签名参数（meta/client 对实现无影响）。
const callQueryFn = () =>
  (repositoriesQueryOptions.queryFn as (arg: unknown) => Promise<{ items: { id: string }[] }>)({});

describe("repositoriesQueryOptions 翻页拉全", () => {
  beforeEach(() => {
    fixtures.apiRequest.mockReset();
  });

  it("仓库不超过一页时不额外请求", async () => {
    fixtures.apiRequest.mockResolvedValueOnce({ items: [repo("a")], page: 1, per_page: 100, total: 1 });
    const page = await callQueryFn();
    expect(page.items).toHaveLength(1);
    expect(fixtures.apiRequest).toHaveBeenCalledTimes(1);
  });

  it("总数超过一页时循环翻页拉全尾部仓库", async () => {
    const page1 = Array.from({ length: 100 }, (_, i) => repo(`a${i}`));
    const page2 = Array.from({ length: 100 }, (_, i) => repo(`b${i}`));
    const page3 = Array.from({ length: 50 }, (_, i) => repo(`c${i}`));
    fixtures.apiRequest
      .mockResolvedValueOnce({ items: page1, page: 1, per_page: 100, total: 250 })
      .mockResolvedValueOnce({ items: page2, page: 2, per_page: 100, total: 250 })
      .mockResolvedValueOnce({ items: page3, page: 3, per_page: 100, total: 250 });
    const page = await callQueryFn();
    // 尾部 50 条不再静默丢失。
    expect(page.items).toHaveLength(250);
    expect(fixtures.apiRequest).toHaveBeenCalledTimes(3);
    expect(fixtures.apiRequest).toHaveBeenNthCalledWith(2, "/api/v1/repositories?per_page=100&page=2");
    expect(fixtures.apiRequest).toHaveBeenNthCalledWith(3, "/api/v1/repositories?per_page=100&page=3");
  });

  it("total 为 0 时返回空列表且只请求一次", async () => {
    fixtures.apiRequest.mockResolvedValueOnce({ items: [], page: 1, per_page: 100, total: 0 });
    const page = await callQueryFn();
    expect(page.items).toHaveLength(0);
    expect(fixtures.apiRequest).toHaveBeenCalledTimes(1);
  });
});

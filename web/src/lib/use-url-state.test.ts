import { renderHook } from "@testing-library/react";
import { act } from "react";
import { afterEach, describe, expect, it } from "vitest";

import { parseIgnoredMode, useUrlState } from "./use-url-state";

afterEach(() => {
  window.history.replaceState(null, "", "/");
});

describe("useUrlState", () => {
  it("URL 无参数时使用默认值", () => {
    const { result } = renderHook(() => useUrlState("state", "open"));
    expect(result.current[0]).toBe("open");
  });

  it("URL 有参数时以参数为初值", () => {
    window.history.replaceState(null, "", "/issues?state=closed");
    const { result } = renderHook(() => useUrlState("state", "open"));
    expect(result.current[0]).toBe("closed");
  });

  it("设置值写入 URL，值等于默认值时移除参数", () => {
    window.history.replaceState(null, "", "/issues?state=closed&repo=acme%2Fdemo");
    const { result } = renderHook(() => useUrlState("state", "open"));
    act(() => result.current[1]("open"));
    const params = new URLSearchParams(window.location.search);
    expect(params.has("state")).toBe(false);
    // 其它参数保留，不被覆盖。
    expect(params.get("repo")).toBe("acme/demo");
  });

  it("设置非默认值后 URL 携带新参数", () => {
    const { result } = renderHook(() => useUrlState("conclusion", ""));
    act(() => result.current[1]("failure"));
    expect(new URLSearchParams(window.location.search).get("conclusion")).toBe("failure");
  });
});

describe("parseIgnoredMode", () => {
  it("仅接受 active/ignored，其余回退 active", () => {
    expect(parseIgnoredMode("ignored")).toBe("ignored");
    expect(parseIgnoredMode("active")).toBe("active");
    expect(parseIgnoredMode("all")).toBe("active");
    expect(parseIgnoredMode("whatever")).toBe("active");
  });
});

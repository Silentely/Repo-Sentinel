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

  it("set 同时更新本地状态（筛选点击即时生效），而不只是改 URL", () => {
    const { result } = renderHook(() => useUrlState("conclusion", ""));
    act(() => result.current[1]("failure"));
    expect(result.current[0]).toBe("failure");
    expect(new URLSearchParams(window.location.search).get("conclusion")).toBe("failure");
    // 恢复默认值：状态回退且 URL 参数被移除。
    act(() => result.current[1](""));
    expect(result.current[0]).toBe("");
    expect(new URLSearchParams(window.location.search).has("conclusion")).toBe(false);
  });

  it("在 window.location 缺失或受限环境安全回退到默认值", () => {
    const originalLocation = window.location;
    try {
      // @ts-expect-error simulate missing location
      delete window.location;
      const { result } = renderHook(() => useUrlState("state", "fallback"));
      expect(result.current[0]).toBe("fallback");
      act(() => result.current[1]("other"));
      expect(result.current[0]).toBe("other");
    } finally {
      window.location = originalLocation;
    }
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

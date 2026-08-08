import { renderHook } from "@testing-library/react";
import { act } from "react";
import { afterEach, beforeEach, describe, expect, it, vi } from "vitest";

import { useAutoDismiss } from "./use-auto-dismiss";

describe("useAutoDismiss", () => {
  beforeEach(() => {
    vi.useFakeTimers();
  });
  afterEach(() => {
    vi.useRealTimers();
  });

  it("show 后立即展示消息", () => {
    const { result } = renderHook(() => useAutoDismiss());
    act(() => result.current[1]("已保存。"));
    expect(result.current[0]).toBe("已保存。");
  });

  it("到达超时后自动清空", () => {
    const { result } = renderHook(() => useAutoDismiss(3000));
    act(() => result.current[1]("已保存。"));
    act(() => {
      vi.advanceTimersByTime(3100);
    });
    expect(result.current[0]).toBe("");
  });

  it("再次 show 重置计时，消息持续存在", () => {
    const { result } = renderHook(() => useAutoDismiss(3000));
    act(() => result.current[1]("第一次"));
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    act(() => result.current[1]("第二次"));
    act(() => {
      vi.advanceTimersByTime(2000);
    });
    expect(result.current[0]).toBe("第二次");
    act(() => {
      vi.advanceTimersByTime(1500);
    });
    expect(result.current[0]).toBe("");
  });
});

import { useCallback, useState } from "react";

/**
 * 将筛选状态同步到 URL 查询参数（history.replaceState，不触发路由导航）：
 * 刷新 / 复制链接后保留当前筛选条件。
 * - 挂载时读取一次 URL 作为初值；值等于默认值时从 URL 移除参数，保持链接干净。
 * - 本地 state 与 URL 双向一致：set 立即更新组件状态并写回 URL（筛选点击即时生效）。
 * - T 默认为 string（普通筛选值）；受限联合类型（如 IgnoredMode）显式传泛型 + parse。
 */
export function useUrlState<T extends string = string>(
  key: string,
  defaultValue: string,
  parse: (raw: string) => T = ((raw) => raw as T),
): [T, (next: T) => void] {
  const [value, setValue] = useState<T>(() => {
    if (typeof window === "undefined" || !window.location) {
      return defaultValue as T;
    }
    const raw = new URLSearchParams(window.location.search).get(key);
    if (raw === null) return defaultValue as T;
    return parse(raw);
  });

  const set = useCallback(
    (next: T) => {
      setValue(next);
      if (typeof window === "undefined" || !window.location) {
        return;
      }
      const params = new URLSearchParams(window.location.search);
      if (next === defaultValue) {
        params.delete(key);
      } else {
        params.set(key, next);
      }
      const qs = params.toString();
      const url = qs ? `${window.location.pathname}?${qs}` : window.location.pathname;
      window.history.replaceState(null, "", url);
    },
    [key, defaultValue],
  );

  return [value, set];
}

/** 列表页共用：ignored 筛选参数解析（"active" | "ignored"，与 IgnoredMode 一致）。 */
export function parseIgnoredMode(raw: string): "active" | "ignored" {
  return raw === "ignored" ? "ignored" : "active";
}

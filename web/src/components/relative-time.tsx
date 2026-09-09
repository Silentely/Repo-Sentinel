import React, { useEffect, useMemo, useState } from "react";

import { formatRelativeTime } from "../lib/format";

// 全局共享时钟：列表页同时渲染大量 RelativeTime 实例时，每实例独立 setInterval
// 会产生 O(行数) 定时器与每分钟同量唤醒；改为单一定时器 + 订阅分发，
// 最后一个实例卸载时自动停表。
let clockListeners = new Set<(now: number) => void>();
let clockTimer: number | null = null;

function subscribeClock(listener: (now: number) => void) {
  clockListeners.add(listener);
  if (clockTimer === null) {
    clockTimer = window.setInterval(() => {
      const now = Date.now();
      for (const l of clockListeners) l(now);
    }, 60_000);
  }
  return () => {
    clockListeners.delete(listener);
    if (clockListeners.size === 0 && clockTimer !== null) {
      window.clearInterval(clockTimer);
      clockTimer = null;
    }
  };
}

/**
 * 相对时间展示：正文渲染「X 分钟前」等相对文案，hover 显示精确绝对时间。
 * 使用 React.memo 缓存不相关的父组件重渲染，同时按分钟刷新当前时间依赖的相对文案。
 * 空值/非法日期渲染为空，与 formatRelativeTime 的返回约定一致，避免列表留白差异。
 * prefix 用于「最后同步: 」这类需要前缀的展示位。
 */
export const RelativeTime = React.memo(function RelativeTime({
  date,
  className,
  prefix = "",
}: {
  date?: string;
  className?: string;
  prefix?: string;
}) {
  // 初始值取自渲染时刻（刷新间隔内保证相对文案正确），之后由共享时钟按分钟推进。
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => subscribeClock(setNow), []);

  const parsed = useMemo(() => {
    if (!date) return null;
    const d = new Date(date);
    if (isNaN(d.getTime())) return null;
    return {
      title: d.toLocaleString("zh-CN"),
      formatted: formatRelativeTime(date, new Date(now)),
    };
  }, [date, now]);

  if (!parsed) return null;

  return (
    <time className={className} dateTime={date} title={parsed.title}>
      {prefix}
      {parsed.formatted}
    </time>
  );
});

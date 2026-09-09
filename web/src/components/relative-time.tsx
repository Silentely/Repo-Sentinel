import React, { useEffect, useMemo, useState } from "react";

import { formatRelativeTime } from "../lib/format";

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
  const [now, setNow] = useState(() => Date.now());
  useEffect(() => {
    const timer = window.setInterval(() => setNow(Date.now()), 60_000);
    return () => window.clearInterval(timer);
  }, []);

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

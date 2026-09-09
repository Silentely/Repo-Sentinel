import React, { useMemo } from "react";

import { formatRelativeTime } from "../lib/format";

/**
 * 相对时间展示：正文渲染「X 分钟前」等相对文案，hover 显示精确绝对时间。
 * 使用 React.memo 与 useMemo 缓存 Intl 本地化格式化开销，防止长列表重渲染时重复格式化。
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
  if (!date) return null;
  const parsed = useMemo(() => {
    const d = new Date(date);
    if (isNaN(d.getTime())) return null;
    return {
      title: d.toLocaleString("zh-CN"),
      formatted: formatRelativeTime(date),
    };
  }, [date]);

  if (!parsed) return null;

  return (
    <time className={className} dateTime={date} title={parsed.title}>
      {prefix}
      {parsed.formatted}
    </time>
  );
});

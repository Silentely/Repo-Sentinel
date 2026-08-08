import { formatRelativeTime } from "../lib/format";

/**
 * 相对时间展示：正文渲染「X 分钟前」等相对文案，hover 显示精确绝对时间。
 * 空值/非法日期渲染为空，与 formatRelativeTime 的返回约定一致，避免列表留白差异。
 * prefix 用于「最后同步: 」这类需要前缀的展示位。
 */
export function RelativeTime({
  date,
  className,
  prefix = "",
}: {
  date?: string;
  className?: string;
  prefix?: string;
}) {
  if (!date) return null;
  const absolute = new Date(date);
  if (isNaN(absolute.getTime())) return null;
  return (
    <time className={className} dateTime={date} title={absolute.toLocaleString("zh-CN")}>
      {prefix}
      {formatRelativeTime(date)}
    </time>
  );
}

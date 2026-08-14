import { memo } from "react";
import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import type { StarTrendPoint } from "./api";

const RANGES = [
  { days: 7, label: "7 天" },
  { days: 30, label: "30 天" },
  { days: 90, label: "90 天" },
  { days: 0, label: "全部" },
] as const;

/** X 轴刻度日期短格式：YYYY-MM-DD → MM-DD，避免长日期挤在一起。 */
function formatXAxisDate(date: string): string {
  const parts = date.split("-");
  if (parts.length !== 3) return date;
  return `${parts[1]}-${parts[2]}`;
}

/** tooltip 值格式化：总数 + 较前一日增量（首日无参照不显示增量）。 */
export function starTotalWithDelta(value: number, delta: number | null): string {
  if (delta == null) return String(value);
  return `${value}（较前日 ${delta >= 0 ? "+" : ""}${delta}）`;
}

/**
 * Y 轴范围：围绕数据上下界外扩 padding（下限不低于 0），
 * 让大基数（如数千 Star）下的个位增长在图中可见，而非从 0 起被压平。
 * padding 取「波动幅度的 2 倍」并夹在 [20, 100]：波动大时贴近「上下 100」的
 * 常规观感，波动只有个位数时自动收紧窗口，避免增长线仍被压成一条直线；
 * 空数据返回安全兜底区间（图表此时不渲染，仅保证调用不越界）。
 */
export function starTrendYDomain(points: StarTrendPoint[]): [number, number] {
  if (points.length === 0) return [0, 100];
  let min = Infinity;
  let max = -Infinity;
  for (const p of points) {
    if (p.total < min) min = p.total;
    if (p.total > max) max = p.total;
  }
  const spread = max - min;
  const pad = Math.max(20, Math.min(100, spread * 2));
  return [Math.max(0, min - pad), max + pad];
}

// memo：页面级 state（重试忙碌/折叠面板等）变化不应让 recharts 图表整图重渲染；
// props 中 points 引用来自查询缓存、days 为数字、onDaysChange 为稳定的 setState。
export const StarTrendChart = memo(function StarTrendChart({
  points,
  days,
  onDaysChange,
  loading,
}: {
  points: StarTrendPoint[];
  days: number;
  onDaysChange: (days: number) => void;
  loading: boolean;
}) {
  const current = points.length > 0 ? points[points.length - 1]?.total : undefined;
  const yDomain = starTrendYDomain(points);
  // 数据跨年（首末日期不同年，days=0 全量场景）时 X 轴刻度带年份，避免 MM-DD 无法区分跨年月份。
  const first = points[0];
  const last = points[points.length - 1];
  const spansYears = Boolean(first && last && first.date.slice(0, 4) !== last.date.slice(0, 4));
  const formatTick = (date: string) => (spansYears ? date.slice(0, 7) : formatXAxisDate(date));
  return (
    <div className="star-trend" data-testid="star-trend">
      <div className="star-trend__head">
        <div>
          <strong>全部仓库 Star 总数</strong>
          {current !== undefined ? <span className="star-trend__value">{current}</span> : null}
        </div>
        <div className="star-trend__ranges" role="group" aria-label="时间范围">
          {RANGES.map((r) => (
            <button
              key={r.days}
              type="button"
              className={`quiet-button quiet-button--compact${days === r.days ? " active" : ""}`}
              onClick={() => onDaysChange(r.days)}
            >
              {r.label}
            </button>
          ))}
        </div>
      </div>
      {points.length === 0 ? (
        loading ? (
          // 加载骨架与图表同高，替代「暂无数据」文案，避免首屏误读为空。
          <>
            <div className="star-trend__skeleton" aria-hidden="true" />
            <span className="sr-only">加载中…</span>
          </>
        ) : (
          <p className="panel-footnote">
            暂无 star 数据。安装 GitHub App 后，对账或 star 事件会自动产生数据。
          </p>
        )
      ) : (
        <ResponsiveContainer width="100%" height={240}>
          <LineChart
            data={points.map((p, i) => ({
              ...p,
              // 当日增量：首日无参照置 null，tooltip 据此决定是否显示。
              delta: i > 0 ? p.total - (points[i - 1]?.total ?? 0) : null,
            }))}
            margin={{ top: 8, right: 16, bottom: 0, left: 0 }}
          >
            <XAxis
              dataKey="date"
              tick={{ fill: "var(--text-secondary)", fontSize: 12 }}
              stroke="var(--border-subtle)"
              minTickGap={28}
              tickFormatter={formatTick}
            />
            <YAxis
              width={44}
              tick={{ fill: "var(--text-secondary)", fontSize: 12 }}
              stroke="var(--border-subtle)"
              allowDecimals={false}
              domain={yDomain}
            />
            <Tooltip
              // 深色主题下 recharts 默认白底/黑字刺眼：跟随设计令牌渲染。
              contentStyle={{
                background: "var(--bg-surface)",
                border: "1px solid var(--border-subtle)",
                borderRadius: "8px",
                color: "var(--text-primary)",
                fontSize: "12px",
              }}
              labelStyle={{ color: "var(--text-secondary)" }}
              labelFormatter={(label) => `日期：${label}`}
              formatter={(value, _name, item) => {
                const delta = (item?.payload as { delta?: number | null } | undefined)?.delta ?? null;
                return [starTotalWithDelta(Number(value), delta), "Star 总数"];
              }}
            />
            <Line
              type="monotone"
              dataKey="total"
              name="Star 总数"
              stroke="var(--accent, #4f6ef2)"
              strokeWidth={2}
              dot={false}
            />
          </LineChart>
        </ResponsiveContainer>
      )}
    </div>
  );
});

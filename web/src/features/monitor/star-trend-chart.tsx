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

export function StarTrendChart({
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
              className={`quiet-button quiet-button--compact${days === r.days ? " is-active" : ""}`}
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
          <LineChart data={points} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
            <XAxis
              dataKey="date"
              tick={{ fontSize: 12 }}
              minTickGap={28}
              tickFormatter={formatXAxisDate}
            />
            <YAxis width={44} tick={{ fontSize: 12 }} allowDecimals={false} />
            <Tooltip labelFormatter={(label) => `日期：${label}`} />
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
}

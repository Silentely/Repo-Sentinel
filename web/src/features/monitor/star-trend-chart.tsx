import { Line, LineChart, ResponsiveContainer, Tooltip, XAxis, YAxis } from "recharts";

import type { StarTrendPoint } from "./api";

const RANGES = [
  { days: 7, label: "7 天" },
  { days: 30, label: "30 天" },
  { days: 90, label: "90 天" },
  { days: 0, label: "全部" },
] as const;

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
        <p className="panel-footnote">
          {loading ? "加载中…" : "暂无 star 数据。安装 GitHub App 后，对账或 star 事件会自动产生数据。"}
        </p>
      ) : (
        <ResponsiveContainer width="100%" height={240}>
          <LineChart data={points} margin={{ top: 8, right: 16, bottom: 0, left: 0 }}>
            <XAxis dataKey="date" tick={{ fontSize: 12 }} />
            <YAxis width={44} tick={{ fontSize: 12 }} allowDecimals={false} />
            <Tooltip />
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

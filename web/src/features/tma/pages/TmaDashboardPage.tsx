import { useEffect, useState } from "react";
import { Link } from "@tanstack/react-router";
import {
  fetchTmaDashboard,
  type TmaDashboardData,
  toggleTmaQuietHours,
} from "../lib/tma-api";
import { tmaHaptics } from "../lib/tma-sdk";

export function TmaDashboardPage() {
  const [dashboard, setDashboard] = useState<TmaDashboardData | null>(null);
  const [loading, setLoading] = useState(true);
  const [error, setError] = useState<string | null>(null);
  const [toastMessage, setToastMessage] = useState<string | null>(null);

  const loadData = (signal?: AbortSignal) => {
    setLoading(true);
    setError(null);
    fetchTmaDashboard(signal)
      .then((data) => {
        setDashboard(data);
        setLoading(false);
      })
      .catch((err) => {
        if (signal?.aborted) return;
        setError(err instanceof Error ? err.message : "获取看板数据失败");
        setLoading(false);
      });
  };

  useEffect(() => {
    const abortCtrl = new AbortController();
    loadData(abortCtrl.signal);
    return () => {
      abortCtrl.abort();
    };
  }, []);

  const handleToggleQuietHours = async () => {
    if (!dashboard) return;
    const nextState = !dashboard.quiet_hours_enabled;
    tmaHaptics.impact("light");

    // Optimistic update
    setDashboard((prev) => (prev ? { ...prev, quiet_hours_enabled: nextState } : prev));

    const abortCtrl = new AbortController();
    try {
      await toggleTmaQuietHours(nextState, abortCtrl.signal);
    } catch {
      // Revert optimistic update
      setDashboard((prev) => (prev ? { ...prev, quiet_hours_enabled: !nextState } : prev));
      tmaHaptics.notification("error");
      setToastMessage("夜间静音设置失败，请重试");
    }
  };

  if (loading) {
    return (
      <div className="flex flex-col items-center justify-center py-16 text-center text-sm text-[var(--tg-theme-hint-color,#94a3b8)]">
        <div className="w-8 h-8 mb-3 border-2 border-current border-t-transparent rounded-full animate-spin opacity-60" />
        <p>加载中…</p>
      </div>
    );
  }

  if (error || !dashboard) {
    return (
      <div className="rounded-xl p-6 text-center bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-rose-500/30">
        <p className="text-sm text-rose-400 mb-4">{error || "加载数据失败"}</p>
        <button
          type="button"
          onClick={() => loadData()}
          className="px-4 py-2 text-xs font-semibold rounded-lg bg-[var(--tg-theme-button-color,#38bdf8)] text-[var(--tg-theme-button-text-color,#ffffff)]"
        >
          重新加载
        </button>
      </div>
    );
  }

  return (
    <div className="space-y-4">
      {/* Header */}
      <header className="flex items-center justify-between pb-2 border-b border-[var(--tg-theme-hint-color,#334155)]/20">
        <div>
          <h1 className="text-lg font-bold text-[var(--tg-theme-text-color,#f8fafc)]">
            RepoSentinel
          </h1>
          <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)]">移动端告警与监控看板</p>
        </div>
      </header>

      {/* Toast Feedback */}
      {toastMessage && (
        <div className="p-3 text-xs rounded-lg bg-rose-500/20 text-rose-300 border border-rose-500/30 flex items-center justify-between">
          <span>{toastMessage}</span>
          <button
            type="button"
            onClick={() => setToastMessage(null)}
            className="text-rose-400 font-bold ml-2"
          >
            ×
          </button>
        </div>
      )}

      {/* KPI Cards */}
      <section className="grid grid-cols-3 gap-2">
        <div className="rounded-xl p-3 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 text-center">
          <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)] mb-1">纳管仓库</p>
          <p className="text-xl font-bold text-[var(--tg-theme-text-color,#f8fafc)]">
            {dashboard.monitored_repos_count}
          </p>
        </div>

        <div className="rounded-xl p-3 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 text-center">
          <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)] mb-1">待办告警</p>
          <p className="text-xl font-bold text-amber-400">
            {dashboard.pending_work_items_count}
          </p>
        </div>

        <div className="rounded-xl p-3 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 text-center">
          <p className="text-xs text-[var(--tg-theme-hint-color,#94a3b8)] mb-1">Webhook 投递</p>
          <p className="text-xl font-bold text-emerald-400">
            {dashboard.webhook_deliveries_total > 0
              ? `${Math.round(dashboard.webhook_delivery_rate * 100)}%`
              : "0%"}
          </p>
          <p className="text-[10px] text-[var(--tg-theme-hint-color,#94a3b8)] mt-0.5">
            {dashboard.webhook_deliveries_success}/{dashboard.webhook_deliveries_total}
          </p>
        </div>
      </section>

      {/* Quiet Hours Switch */}
      <section className="rounded-xl p-3.5 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 flex items-center justify-between">
        <div>
          <div className="flex items-center space-x-1.5">
            <span className="text-base">🌙</span>
            <span className="text-sm font-medium text-[var(--tg-theme-text-color,#f8fafc)]">
              夜间静音 (Quiet Hours)
            </span>
          </div>
          {dashboard.quiet_hours_start && dashboard.quiet_hours_end && (
            <p className="text-[11px] text-[var(--tg-theme-hint-color,#94a3b8)] mt-0.5 ml-6">
              {dashboard.quiet_hours_start} - {dashboard.quiet_hours_end} ({dashboard.quiet_hours_tz || "UTC"})
            </p>
          )}
        </div>

        <button
          type="button"
          role="switch"
          aria-label="夜间静音"
          aria-checked={dashboard.quiet_hours_enabled ? "true" : "false"}
          onClick={handleToggleQuietHours}
          className={`relative inline-flex h-6 w-11 flex-shrink-0 cursor-pointer rounded-full border-2 border-transparent transition-colors duration-200 ease-in-out focus:outline-none ${
            dashboard.quiet_hours_enabled
              ? "bg-[var(--tg-theme-button-color,#38bdf8)]"
              : "bg-slate-700"
          }`}
        >
          <span
            className={`pointer-events-none inline-block h-5 w-5 transform rounded-full bg-white shadow ring-0 transition duration-200 ease-in-out ${
              dashboard.quiet_hours_enabled ? "translate-x-5" : "translate-x-0"
            }`}
          />
        </button>
      </section>

      {/* Active Work Items / Events List */}
      <section className="space-y-2">
        <h2 className="text-xs font-semibold text-[var(--tg-theme-hint-color,#94a3b8)] uppercase tracking-wider px-1">
          待处理事项与活跃告警 ({dashboard.work_items.length})
        </h2>

        {dashboard.work_items.length === 0 ? (
          <div className="rounded-xl p-6 text-center bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 text-xs text-[var(--tg-theme-hint-color,#94a3b8)]">
            <p className="text-2xl mb-1">🎉</p>
            <p>暂无待处理告警与待办项</p>
          </div>
        ) : (
          <div className="space-y-2">
            {dashboard.work_items.map((item) => (
              <Link
                key={item.id}
                to="/tma/events/$id"
                params={{ id: item.id }}
                className="block rounded-xl p-3 bg-[var(--tg-theme-secondary-bg-color,#1e293b)] border border-[var(--tg-theme-hint-color,#334155)]/20 active:scale-[0.99] transition-transform hover:border-[var(--tg-theme-button-color,#38bdf8)]/50"
              >
                <div className="flex items-start justify-between gap-2">
                  <div className="flex-1 min-w-0">
                    <div className="flex items-center space-x-1.5 mb-1">
                      <span className="text-xs font-mono font-medium text-sky-400">
                        #{item.number}
                      </span>
                      <span className="text-[10px] uppercase font-semibold px-1.5 py-0.5 rounded bg-slate-800 text-slate-300">
                        {item.kind}
                      </span>
                      <span
                        className={`text-[10px] px-1.5 py-0.5 rounded ${
                          item.state === "open"
                            ? "bg-emerald-500/20 text-emerald-300"
                            : "bg-slate-700 text-slate-400"
                        }`}
                      >
                        {item.state}
                      </span>
                    </div>
                    <p className="text-sm font-medium text-[var(--tg-theme-text-color,#f8fafc)] truncate">
                      {item.title}
                    </p>
                  </div>
                  <span className="text-[var(--tg-theme-hint-color,#94a3b8)] text-base self-center">
                    ›
                  </span>
                </div>
              </Link>
            ))}
          </div>
        )}
      </section>
    </div>
  );
}

export default TmaDashboardPage;

import { useMemo, useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { CollapsiblePanel } from "../../components/collapsible-panel";
import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { RelativeTime } from "../../components/relative-time";
import { toApiError } from "../../lib/api/errors";
import {
  channelLabel,
  eventActionLabel,
  eventKindLabel,
  outboxStatusLabel,
  severityLabel,
} from "../../lib/format";
import {
  DASHBOARD_FEED_LIMIT,
  dashboardQueryOptions,
  eventsQueryOptions,
  githubConfigQueryOptions,
  outboxQueryOptions,
  repositoriesQueryOptions,
  retryOutbox,
  settingsQueryOptions,
  starTrendQueryOptions,
} from "./api";
import { StarTrendChart } from "./star-trend-chart";

type PanelKey = "outbox" | "events" | "stars";

const PANEL_STORAGE_KEY = "reposentinel-dashboard-panels";

const DEFAULT_OPEN_PANELS: Record<PanelKey, boolean> = {
  outbox: true,
  events: true,
  stars: true,
};

function readOpenPanels(): Record<PanelKey, boolean> {
  if (typeof window === "undefined") return { ...DEFAULT_OPEN_PANELS };
  try {
    const raw = window.localStorage.getItem(PANEL_STORAGE_KEY);
    if (!raw) return { ...DEFAULT_OPEN_PANELS };
    const parsed = JSON.parse(raw) as Partial<Record<PanelKey, unknown>>;
    return {
      outbox: typeof parsed.outbox === "boolean" ? parsed.outbox : DEFAULT_OPEN_PANELS.outbox,
      events: typeof parsed.events === "boolean" ? parsed.events : DEFAULT_OPEN_PANELS.events,
      stars: typeof parsed.stars === "boolean" ? parsed.stars : DEFAULT_OPEN_PANELS.stars,
    };
  } catch {
    return { ...DEFAULT_OPEN_PANELS };
  }
}

function writeOpenPanels(next: Record<PanelKey, boolean>) {
  try {
    window.localStorage.setItem(PANEL_STORAGE_KEY, JSON.stringify(next));
  } catch {
    // 隐私模式或配额满时静默忽略，不影响页面使用。
  }
}

export function DashboardPage() {
  const queryClient = useQueryClient();
  const dashboard = useQuery(dashboardQueryOptions);
  const repos = useQuery(repositoriesQueryOptions);
  const events = useQuery(eventsQueryOptions(DASHBOARD_FEED_LIMIT));
  const outbox = useQuery(outboxQueryOptions("", "", DASHBOARD_FEED_LIMIT));
  const settings = useQuery(settingsQueryOptions);
  const githubConfig = useQuery(githubConfigQueryOptions);

  // Star 增长趋势：时间范围由本地状态管理，切换范围时按 queryKey 换缓存。
  const [trendDays, setTrendDays] = useState(30);
  const starTrend = useQuery(starTrendQueryOptions(trendDays));
  const featureStars = settings.data?.["feature.stars"] !== false;

  // 折叠状态从 localStorage 恢复，切换后写回。
  const [openPanels, setOpenPanels] = useState<Record<PanelKey, boolean>>(readOpenPanels);
  // 行级忙碌：只让当前操作的行转圈。
  const [retryBusyId, setRetryBusyId] = useState<string | null>(null);

  const invalidateOutboxAndDashboard = async () => {
    await queryClient.invalidateQueries({ queryKey: ["outbox"] });
    await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
  };

  const retry = useMutation({
    mutationFn: retryOutbox,
    onMutate: (id) => setRetryBusyId(id),
    onSettled: () => setRetryBusyId(null),
    onSuccess: invalidateOutboxAndDashboard,
  });

  const stats = dashboard.data;
  const featureIssues = settings.data?.["feature.issues"] !== false;
  const featurePRs = settings.data?.["feature.pull_requests"] !== false;
  const featureActions = settings.data?.["feature.actions"] !== false;
  const featureAlerts = settings.data?.["feature.security_alerts"] !== false;

  const repoItems = repos.data?.items ?? [];
  // 仓库与基线：排除已归档，避免归档仓继续占位；派生数据缓存避免每渲染重建。
  const visibleRepos = useMemo(
    () => repoItems.filter((r) => !r.is_archived && r.sync_status !== "archived"),
    [repoItems],
  );
  const baselineRepos = useMemo(
    () => visibleRepos.filter((r) => r.sync_status === "baseline_sync"),
    [visibleRepos],
  );
  const repoNameMap = useMemo(
    () => Object.fromEntries(repoItems.map((r) => [r.id, r.full_name || `${r.owner}/${r.name}`])),
    [repoItems],
  );

  const eventItems = events.data?.items ?? [];
  const outboxItems = outbox.data?.items ?? [];
  // 面板脚注用总数判断是否被截断（total > 展示上限时提示「仅显示最近 N 条」）。
  const eventTotal = events.data?.total ?? 0;
  const outboxTotal = outbox.data?.total ?? 0;

  // 最近事件是混合流（Issue/PR/Actions/告警），脚注提供分类快捷入口按需跳转；
  // 按功能开关过滤，避免入口指向被禁用的列表页。
  const kindQuickLinks = [
    featureIssues && { to: "/issues", label: "Issues" },
    featurePRs && { to: "/pull-requests", label: "Pull Requests" },
    featureActions && { to: "/actions", label: "Actions" },
    featureAlerts && { to: "/security", label: "安全告警" },
  ].filter(Boolean) as { to: string; label: string }[];

  const cfg = githubConfig.data;
  const inboundReady = Boolean(cfg?.webhook_secret_configured);
  const outboundReady = Boolean(cfg?.app_id_configured && cfg?.private_key_configured);
  const hasRepos = visibleRepos.length > 0;
  const hasActiveRepo = visibleRepos.some((r) => r.sync_status === "active");
  const channelsEnabled = (stats?.channels_enabled ?? 0) > 0;
  const setupSteps = [
    {
      id: "inbound",
      ok: inboundReady,
      label: "入站 Webhook Secret",
      hint: "GitHub → 本服务验签",
      to: "/github" as const,
    },
    {
      id: "outbound",
      ok: outboundReady,
      label: "出站 App 凭据",
      hint: "App ID + 私钥，用于对账/同步",
      to: "/github" as const,
    },
    {
      id: "repos",
      ok: hasRepos,
      label: "已关注仓库",
      hint: "安装 App 后点「从 GitHub 同步仓库」",
      to: "/github" as const,
    },
    {
      id: "baseline",
      ok: hasRepos && (hasActiveRepo || baselineRepos.length === 0),
      label: "基线已放行",
      hint: "对账成功会自动结束基线，也可手动「立即放行」",
      to: "/" as const,
    },
    {
      id: "channels",
      ok: channelsEnabled,
      label: "通知渠道",
      hint: "Telegram 或 HTTP Webhook",
      to: "/notifications" as const,
    },
  ];
  const setupDone = setupSteps.filter((s) => s.ok).length;
  const showSetup = setupDone < setupSteps.length;

  function togglePanel(key: PanelKey) {
    setOpenPanels((prev) => {
      const next = { ...prev, [key]: !prev[key] };
      writeOpenPanels(next);
      return next;
    });
  }

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">值守概览</p>
          <h1>现在是否健康，今天发生了什么。</h1>
          <p>Webhook 入库后会在此汇总。仓库对账与基线放行请在「设置」中操作。</p>
        </div>
      </section>

      {dashboard.isError ? (
        <ErrorAlert
          title="无法加载仪表盘"
          message={toApiError(dashboard.error).message}
          errorCode={toApiError(dashboard.error).errorCode}
        />
      ) : null}

      {showSetup ? (
        <section className="onboarding-card setup-progress" aria-labelledby="setup-progress-title">
          <div className="onboarding-card__header">
            <h2 id="setup-progress-title">接入进度</h2>
            <span className="muted">
              {setupDone} / {setupSteps.length} 完成
            </span>
          </div>
          <p className="field-hint">按步骤完成即可开始值守；已完成的步骤会打勾。</p>
          <ol className="setup-progress__list">
            {setupSteps.map((step, index) => (
              <li key={step.id} className={`setup-progress__item${step.ok ? " is-done" : ""}`}>
                <span className="setup-progress__index" aria-hidden="true">
                  {step.ok ? "✓" : index + 1}
                </span>
                <div className="setup-progress__body">
                  <strong>{step.label}</strong>
                  <span className="muted">{step.hint}</span>
                </div>
                {!step.ok && step.to !== "/" ? (
                  <Link className="quiet-button quiet-button--compact" to={step.to}>
                    去配置
                  </Link>
                ) : null}
              </li>
            ))}
          </ol>
        </section>
      ) : null}

      <section className="status-card" aria-label="关键指标">
        <div className="status-grid">
          {featureIssues ? (
            <Metric label="开放 Issue" value={stats?.open_issues} to="/issues" loading={dashboard.isPending} />
          ) : null}
          {featurePRs ? <Metric label="开放 PR" value={stats?.open_pulls} to="/pull-requests" loading={dashboard.isPending} /> : null}
          {featureActions ? (
            <Metric label="失败 Actions" value={stats?.failed_actions} to="/actions" loading={dashboard.isPending} />
          ) : null}
          {featureAlerts ? (
            <Metric label="开放安全告警" value={stats?.open_security} to="/security" loading={dashboard.isPending} />
          ) : null}
          <Metric label="24h 事件" value={stats?.events_24h} loading={dashboard.isPending} />
          {/* 跳转带 status=dead：outbox 页按 URL 初始筛选，直达失败投递列表。 */}
          <Metric label="投递失败" value={stats?.outbox_dead} to="/notifications/outbox?status=dead" loading={dashboard.isPending} />
        </div>
        <div className="status-grid status-grid--secondary">
          <Metric label="活跃仓库" value={stats?.repos_active} to="/repos" loading={dashboard.isPending} />
          <Metric label="基线中" value={stats?.repos_baseline} loading={dashboard.isPending} />
          <Metric label="已启用渠道" value={stats?.channels_enabled} to="/notifications" loading={dashboard.isPending} />
        </div>
      </section>

      {/* 顺序：Star 增长 → 通知投递 → 最近事件 → 仓库与基线 */}
      {featureStars ? (
        <CollapsiblePanel
          id="stars"
          title="⭐ Star 增长"
          open={openPanels.stars}
          onToggle={() => togglePanel("stars")}
        >
          <StarTrendChart
            points={starTrend.data ?? []}
            days={trendDays}
            onDaysChange={setTrendDays}
            loading={starTrend.isPending}
          />
        </CollapsiblePanel>
      ) : null}
      <CollapsiblePanel
        id="outbox"
        title="通知投递"
        count={
          outboxTotal > DASHBOARD_FEED_LIMIT ? `${DASHBOARD_FEED_LIMIT}+` : outboxItems.length
        }
        open={openPanels.outbox}
        onToggle={() => togglePanel("outbox")}
      >
        <QueryGate
          query={outbox}
          errorTitle="无法加载投递记录"
          isEmpty={outboxItems.length === 0}
          emptyState={
            <EmptyState title="还没有投递记录" description="配置 Telegram 或 HTTP Webhook 渠道后，实时通知会进入 Outbox。" />
          }
        >
          <ul className="feed-list">
            {outboxItems.map((item) => (
              <li key={item.id} className="feed-row">
                <div className="feed-row__main">
                  <div className="feed-row__meta">
                    <span className={`event-kind status-${item.status}`}>{outboxStatusLabel(item.status)}</span>
                    {item.channel_type ? (
                      <span className="muted channel-tag">{channelLabel(item.channel_type)}</span>
                    ) : null}
                    {item.created_at ? <RelativeTime date={item.created_at} className="event-time" /> : null}
                    <span className="muted">尝试 {item.attempt_count} 次</span>
                    {item.last_error_code ? <span className="error-code">{item.last_error_code}</span> : null}
                  </div>
                  <strong className="feed-row__title" title={item.title || item.id}>
                    {item.title || item.id}
                  </strong>
                </div>
                <div className="feed-row__actions">
                  {item.status === "dead" ? (
                    <button
                      className="quiet-button quiet-button--compact"
                      type="button"
                      onClick={() => retry.mutate(item.id)}
                      disabled={retryBusyId === item.id}
                    >
                      {retryBusyId === item.id ? "重试中…" : "重试"}
                    </button>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
          {outboxTotal > DASHBOARD_FEED_LIMIT ? (
            <p className="panel-footnote">
              共 {outboxTotal} 条投递记录，仅显示最近 {DASHBOARD_FEED_LIMIT} 条。
              <Link to="/notifications/outbox">查看全部</Link>
            </p>
          ) : null}
        </QueryGate>
      </CollapsiblePanel>

      <CollapsiblePanel
        id="events"
        title="最近事件"
        count={
          eventTotal > DASHBOARD_FEED_LIMIT ? `${DASHBOARD_FEED_LIMIT}+` : eventItems.length
        }
        open={openPanels.events}
        onToggle={() => togglePanel("events")}
      >
        <QueryGate
          query={events}
          errorTitle="无法加载事件"
          isEmpty={eventItems.length === 0}
          emptyState={
            <EmptyState
              title="还没有事件"
              description="配置 GitHub Webhook 后，Issue / PR / Actions / 安全告警会出现在这里。"
            />
          }
        >
          <ul className="feed-list">
            {eventItems.map((ev) => {
              const repoName = ev.repository_id ? repoNameMap[ev.repository_id] : undefined;
              const title = ev.title || "（无标题）";
              return (
                <li key={ev.id} className="feed-row">
                  <div className="feed-row__main">
                    <div className="feed-row__meta">
                      <span className={`event-kind kind-${ev.kind}`}>{eventKindLabel(ev.kind)}</span>
                      <span className="event-action">{eventActionLabel(ev.action, ev.kind)}</span>
                      {repoName ? (
                        <span className="event-repo" title={repoName}>
                          {repoName}
                        </span>
                      ) : null}
                      {ev.severity ? <span className={`severity severity-${ev.severity}`}>{severityLabel(ev.severity)}</span> : null}
                      {ev.occurred_at ? <RelativeTime date={ev.occurred_at} className="event-time" /> : null}
                      {ev.actor ? <span className="muted">· {ev.actor}</span> : null}
                    </div>
                    <strong className="feed-row__title" title={title}>
                      {title}
                    </strong>
                  </div>
                  <div className="feed-row__actions">
                    {ev.html_url ? (
                      <a className="quiet-button quiet-button--compact" href={ev.html_url} target="_blank" rel="noreferrer">
                        打开
                      </a>
                    ) : null}
                  </div>
                </li>
              );
            })}
          </ul>
          {eventItems.length > 0 ? (
            <p className="panel-footnote">
              {eventTotal > DASHBOARD_FEED_LIMIT
                ? `共 ${eventTotal} 条事件，仅显示最近 ${DASHBOARD_FEED_LIMIT} 条。`
                : `共 ${eventTotal} 条事件。`}
              {kindQuickLinks.length > 0 ? (
                <>
                  {" "}
                  分类查看：
                  {kindQuickLinks.map((link, index) => (
                    <span key={link.to}>
                      {index > 0 ? " · " : null}
                      <Link to={link.to}>{link.label}</Link>
                    </span>
                  ))}
                </>
              ) : null}
            </p>
          ) : null}
        </QueryGate>
      </CollapsiblePanel>
    </>
  );
}

function Metric({ label, value, to, loading }: { label: string; value?: number; to?: string; loading?: boolean }) {
  const inner = (
    <>
      <span className="status-item__label">{label}</span>
      {/* 加载中显示骨架而非「—」：避免把"尚未加载"误读为指标为 0/无数据。 */}
      {loading ? (
        <span className="status-item__skeleton" aria-hidden="true" />
      ) : (
        <strong>{value === undefined ? "—" : value}</strong>
      )}
    </>
  );
  if (to) {
    return (
      <Link className="status-item status-item--link" to={to}>
        {inner}
      </Link>
    );
  }
  return <div className="status-item">{inner}</div>;
}


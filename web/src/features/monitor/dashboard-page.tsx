import { useState, type ReactNode } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, ChevronDown, CircleDashed, ExternalLink } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
import { formatRelativeTime } from "../../lib/format";
import {
  activateRepository,
  dashboardQueryOptions,
  eventsQueryOptions,
  outboxQueryOptions,
  reconcileAll,
  reconcileRepository,
  repositoriesQueryOptions,
  retryOutbox,
} from "./api";

type PanelKey = "outbox" | "events" | "repos";

const PANEL_STORAGE_KEY = "reposentinel-dashboard-panels";

const DEFAULT_OPEN_PANELS: Record<PanelKey, boolean> = {
  outbox: true,
  events: true,
  repos: true,
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
      repos: typeof parsed.repos === "boolean" ? parsed.repos : DEFAULT_OPEN_PANELS.repos,
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
  const events = useQuery(eventsQueryOptions);
  const outbox = useQuery(outboxQueryOptions);

  // 折叠状态从 localStorage 恢复，切换后写回。
  const [openPanels, setOpenPanels] = useState<Record<PanelKey, boolean>>(readOpenPanels);

  const activate = useMutation({
    mutationFn: activateRepository,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["repositories"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });
  const reconcileOne = useMutation({
    mutationFn: reconcileRepository,
  });
  const reconcileEverything = useMutation({
    mutationFn: reconcileAll,
  });
  const retry = useMutation({
    mutationFn: retryOutbox,
    onSuccess: async () => {
      await queryClient.invalidateQueries({ queryKey: ["outbox"] });
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const stats = dashboard.data;
  // 仓库与基线：排除已归档，避免归档仓继续占位。
  const visibleRepos = (repos.data?.items ?? []).filter(
    (r) => !r.is_archived && r.sync_status !== "archived",
  );
  const baselineRepos = visibleRepos.filter((r) => r.sync_status === "baseline_sync");
  const repoNameMap = Object.fromEntries(
    (repos.data?.items ?? []).map((r) => [r.id, r.full_name || `${r.owner}/${r.name}`]),
  );

  const eventItems = events.data?.items ?? [];
  const outboxItems = outbox.data?.items ?? [];

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
          <p>Webhook 入库后会在此汇总。基线中的仓库不会发送历史通知洪流。</p>
        </div>
      </section>

      {dashboard.isError ? (
        <ErrorAlert
          title="无法加载仪表盘"
          message={toApiError(dashboard.error).message}
          errorCode={toApiError(dashboard.error).errorCode}
        />
      ) : null}

      <section className="status-card" aria-label="关键指标">
        <div className="status-grid">
          <Metric label="开放 Issue" value={stats?.open_issues} />
          <Metric label="开放 PR" value={stats?.open_pulls} />
          <Metric label="失败 Actions" value={stats?.failed_actions} />
          <Metric label="开放安全告警" value={stats?.open_security} />
          <Metric label="24h 事件" value={stats?.events_24h} />
          <Metric label="投递失败" value={stats?.outbox_dead} />
        </div>
        <div className="status-grid status-grid--secondary">
          <Metric label="活跃仓库" value={stats?.repos_active} />
          <Metric label="基线中" value={stats?.repos_baseline} />
          <Metric label="已启用渠道" value={stats?.channels_enabled} />
        </div>
      </section>

      {/* 顺序：通知投递 → 最近事件 → 仓库与基线 */}
      <CollapsiblePanel
        id="outbox"
        title="通知投递"
        count={outboxItems.length}
        open={openPanels.outbox}
        onToggle={() => togglePanel("outbox")}
      >
        {outboxItems.length === 0 ? (
          <EmptyState title="还没有投递记录" description="配置 Telegram 或 HTTP Webhook 渠道后，实时通知会进入 Outbox。" />
        ) : (
          <ul className="feed-list">
            {outboxItems.map((item) => (
              <li key={item.id} className="feed-row">
                <div className="feed-row__main">
                  <div className="feed-row__meta">
                    <span className={`event-kind status-${item.status}`}>{item.status}</span>
                    {item.created_at ? <span className="event-time">{formatRelativeTime(item.created_at)}</span> : null}
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
                      disabled={retry.isPending}
                    >
                      重试
                    </button>
                  ) : null}
                </div>
              </li>
            ))}
          </ul>
        )}
      </CollapsiblePanel>

      <CollapsiblePanel
        id="events"
        title="最近事件"
        count={eventItems.length}
        open={openPanels.events}
        onToggle={() => togglePanel("events")}
      >
        {eventItems.length === 0 ? (
          <EmptyState
            title="还没有事件"
            description="配置 GitHub Webhook 后，Issue / PR / Actions / 安全告警会出现在这里。"
          />
        ) : (
          <ul className="feed-list">
            {eventItems.map((ev) => {
              const repoName = ev.repository_id ? repoNameMap[ev.repository_id] : undefined;
              const title = ev.title || "（无标题）";
              return (
                <li key={ev.id} className="feed-row">
                  <div className="feed-row__main">
                    <div className="feed-row__meta">
                      <span className={`event-kind kind-${ev.kind}`}>{formatEventKind(ev.kind)}</span>
                      <span className="event-action">{formatEventAction(ev.action)}</span>
                      {repoName ? (
                        <span className="event-repo" title={repoName}>
                          {repoName}
                        </span>
                      ) : null}
                      {ev.severity ? <span className={`severity severity-${ev.severity}`}>{ev.severity}</span> : null}
                      {ev.occurred_at ? <span className="event-time">{formatRelativeTime(ev.occurred_at)}</span> : null}
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
        )}
      </CollapsiblePanel>

      <CollapsiblePanel
        id="repos"
        title="仓库与基线"
        count={visibleRepos.length}
        open={openPanels.repos}
        onToggle={() => togglePanel("repos")}
        headerExtra={
          <button
            className="quiet-button quiet-button--compact"
            type="button"
            disabled={reconcileEverything.isPending}
            onClick={(e) => {
              e.stopPropagation();
              reconcileEverything.mutate();
            }}
          >
            {reconcileEverything.isPending ? "对账排队中…" : "立即对账全部自有仓"}
          </button>
        }
      >
        {repos.isPending ? <p className="muted">加载仓库…</p> : null}
        {!repos.isPending && visibleRepos.length === 0 ? (
          <EmptyState
            title="还没有关注中的仓库"
            description="安装 GitHub App 并配置 Webhook 后，仓库会自动出现。已归档仓库请在「仓库管理」查看。"
            action={
              <span className="link-row" style={{ justifyContent: "center", flexWrap: "wrap" }}>
                <Link to="/github">打开 GitHub App 页</Link>
                <span className="muted">· 安装后点「从 GitHub 同步仓库」可补拉仓库</span>
              </span>
            }
          />
        ) : null}
        {visibleRepos.length > 0 ? (
          <ul className="repo-baseline-list">
            {visibleRepos.map((repo) => {
              const name = repo.full_name || `${repo.owner}/${repo.name}`.replace(/^\/|\/$/g, "") || repo.id;
              const isActive = repo.sync_status === "active";
              const isBaseline = repo.sync_status === "baseline_sync";
              return (
                <li key={repo.id} className="repo-baseline-row" data-state={isActive ? "done" : "next"}>
                  <span className="repo-baseline-row__icon" aria-hidden="true">
                    {isActive ? <CheckCircle2 size={18} /> : <CircleDashed size={18} />}
                  </span>
                  <div className="repo-baseline-row__body">
                    <strong className="repo-baseline-row__name" title={name}>
                      {name}
                    </strong>
                    <span className="repo-baseline-row__meta">
                      {syncLabel(repo.sync_status || "")}
                      <span aria-hidden="true"> · </span>
                      {repo.type === "external_public" ? "外部" : "自有"}
                    </span>
                  </div>
                  <div className="repo-baseline-row__actions">
                    {repo.html_url ? (
                      <a className="quiet-button quiet-button--compact" href={repo.html_url} target="_blank" rel="noreferrer">
                        <ExternalLink size={14} aria-hidden="true" />
                        <span>GitHub</span>
                      </a>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                    {repo.type !== "external_public" ? (
                      <button
                        className="quiet-button quiet-button--compact"
                        type="button"
                        disabled={reconcileOne.isPending}
                        onClick={() => reconcileOne.mutate(repo.id)}
                      >
                        对账
                      </button>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                    {isBaseline ? (
                      <button
                        className="quiet-button quiet-button--compact quiet-button--primary-ghost"
                        type="button"
                        disabled={activate.isPending}
                        onClick={() => activate.mutate(repo.id)}
                      >
                        完成基线
                      </button>
                    ) : (
                      <span className="repo-baseline-row__slot" aria-hidden="true" />
                    )}
                  </div>
                </li>
              );
            })}
          </ul>
        ) : null}
        {baselineRepos.length > 0 ? (
          <p className="panel-footnote">基线中的仓库会抑制实时通知，避免首次同步洪流。确认快照就绪后点击「完成基线」。</p>
        ) : null}
      </CollapsiblePanel>
    </>
  );
}

function CollapsiblePanel({
  id,
  title,
  count,
  open,
  onToggle,
  headerExtra,
  children,
}: {
  id: string;
  title: string;
  count?: number;
  open: boolean;
  onToggle: () => void;
  headerExtra?: ReactNode;
  children: ReactNode;
}) {
  const headingId = `${id}-title`;
  const panelId = `${id}-panel`;
  return (
    <section className={`onboarding-card collapsible-panel${open ? " is-open" : ""}`} aria-labelledby={headingId}>
      <div className="collapsible-panel__header">
        <button
          type="button"
          className="collapsible-panel__toggle"
          aria-expanded={open}
          aria-controls={panelId}
          onClick={onToggle}
        >
          <ChevronDown className="collapsible-panel__chevron" size={18} aria-hidden="true" />
          <h2 id={headingId}>{title}</h2>
          {typeof count === "number" ? <span className="collapsible-panel__count">{count}</span> : null}
        </button>
        {headerExtra ? <div className="collapsible-panel__extra">{headerExtra}</div> : null}
      </div>
      {open ? (
        <div id={panelId} className="collapsible-panel__body">
          {children}
        </div>
      ) : null}
    </section>
  );
}

function Metric({ label, value }: { label: string; value?: number }) {
  return (
    <div className="status-item">
      <span className="status-item__label">{label}</span>
      <strong>{value === undefined ? "—" : value}</strong>
    </div>
  );
}

function syncLabel(status: string): string {
  switch (status) {
    case "baseline_sync":
      return "基线中";
    case "active":
      return "正常";
    case "archived":
      return "已归档";
    case "unavailable":
      return "不可用";
    default:
      return status;
  }
}

function formatEventKind(kind: string): string {
  switch (kind) {
    case "issue":
      return "Issue";
    case "pull_request":
      return "PR";
    case "workflow_run":
      return "Actions";
    case "dependabot":
      return "Dependabot";
    case "code_scanning":
      return "Code Scan";
    case "secret_scanning":
      return "Secret";
    default:
      return kind;
  }
}

function formatEventAction(action: string): string {
  switch (action) {
    case "opened":
      return "打开";
    case "closed":
      return "关闭";
    case "reopened":
      return "重新打开";
    case "completed":
      return "完成";
    case "recovered":
      return "恢复";
    case "updated":
      return "更新";
    case "created":
      return "创建";
    case "dismissed":
      return "忽略";
    case "fixed":
      return "修复";
    default:
      return action;
  }
}


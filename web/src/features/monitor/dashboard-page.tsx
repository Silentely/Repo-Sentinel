import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { CheckCircle2, CircleDashed, ExternalLink } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { toApiError } from "../../lib/api/errors";
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

export function DashboardPage() {
  const queryClient = useQueryClient();
  const dashboard = useQuery(dashboardQueryOptions);
  const repos = useQuery(repositoriesQueryOptions);
  const events = useQuery(eventsQueryOptions);
  const outbox = useQuery(outboxQueryOptions);

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
  const baselineRepos = repos.data?.items.filter((r) => r.sync_status === "baseline_sync") ?? [];

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
          <Metric label="通知死信" value={stats?.outbox_dead} />
        </div>
        <div className="status-grid" style={{ marginTop: "1rem" }}>
          <Metric label="活跃仓库" value={stats?.repos_active} />
          <Metric label="基线中" value={stats?.repos_baseline} />
          <Metric label="已启用渠道" value={stats?.channels_enabled} />
        </div>
      </section>

      <section className="onboarding-card" aria-labelledby="baseline-title">
        <div className="onboarding-card__header">
          <h2 id="baseline-title">仓库与基线</h2>
          <button
            className="quiet-button"
            type="button"
            disabled={reconcileEverything.isPending}
            onClick={() => reconcileEverything.mutate()}
          >
            {reconcileEverything.isPending ? "对账排队中…" : "立即对账全部自有仓"}
          </button>
        </div>
        {repos.isPending ? <p>加载仓库…</p> : null}
        {repos.data && repos.data.items.length === 0 ? (
          <EmptyState
            title="还没有仓库"
            description="安装 GitHub App 并配置 Webhook 后，仓库会自动出现。也可在 GitHub App 页登记外部公开仓库。"
            action={
              <span className="link-row" style={{ justifyContent: "center", flexWrap: "wrap" }}>
                <Link to="/github">打开 GitHub App 页</Link>
                <span className="muted">· 安装后点「从 GitHub 同步仓库」可补拉仓库</span>
              </span>
            }
          />
        ) : null}
        <ul className="onboarding-list">
          {(repos.data?.items ?? []).map((repo) => (
            <li key={repo.id} data-state={repo.sync_status === "active" ? "done" : "next"}>
              {repo.sync_status === "active" ? (
                <CheckCircle2 aria-hidden="true" size={18} />
              ) : (
                <CircleDashed aria-hidden="true" size={18} />
              )}
              <span>
                <strong>{repo.full_name || `${repo.owner}/${repo.name}`.replace(/^\/|\/$/g, "") || repo.id}</strong>
                <span className="muted">
                  {" "}
                  · {syncLabel(repo.sync_status || "")} · {repo.type === "external_public" ? "外部" : "自有"}
                </span>
              </span>
              {repo.sync_status === "baseline_sync" ? (
                <button
                  className="quiet-button"
                  type="button"
                  disabled={activate.isPending}
                  onClick={() => activate.mutate(repo.id)}
                >
                  完成基线
                </button>
              ) : null}
              {repo.type !== "external_public" ? (
                <button
                  className="quiet-button"
                  type="button"
                  disabled={reconcileOne.isPending}
                  onClick={() => reconcileOne.mutate(repo.id)}
                >
                  对账
                </button>
              ) : null}
              {repo.html_url ? (
                <a className="quiet-button" href={repo.html_url} target="_blank" rel="noreferrer">
                  <ExternalLink size={14} aria-hidden="true" /> GitHub
                </a>
              ) : null}
            </li>
          ))}
        </ul>
        {baselineRepos.length > 0 ? (
          <p className="muted">基线中的仓库会抑制实时通知，避免首次同步洪流。确认快照就绪后点击「完成基线」。</p>
        ) : null}
      </section>

      <section className="onboarding-card" aria-labelledby="events-title">
        <h2 id="events-title">最近事件</h2>
        {(events.data?.items ?? []).length === 0 ? (
          <EmptyState title="还没有事件" description="配置 GitHub Webhook 后，Issue / PR / Actions / 安全告警会出现在这里。" />
        ) : (
          <ul className="event-list">
            {(events.data?.items ?? []).map((ev) => (
              <li key={ev.id}>
                <span className="event-kind">{ev.kind}</span>
                <span>{ev.action}</span>
                <strong>{ev.title || "（无标题）"}</strong>
                {ev.html_url ? (
                  <a href={ev.html_url} target="_blank" rel="noreferrer">
                    打开
                  </a>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section className="onboarding-card" aria-labelledby="outbox-title">
        <h2 id="outbox-title">通知投递</h2>
        {(outbox.data?.items ?? []).length === 0 ? (
          <EmptyState title="还没有投递记录" description="配置 Telegram 或 HTTP Webhook 渠道后，实时通知会进入 Outbox。" />
        ) : (
          <ul className="event-list">
            {(outbox.data?.items ?? []).map((item) => (
              <li key={item.id}>
                <span className="event-kind">{item.status}</span>
                <strong>{item.title || item.id}</strong>
                <span className="muted">尝试 {item.attempt_count}</span>
                {item.status === "dead" ? (
                  <button className="quiet-button" type="button" onClick={() => retry.mutate(item.id)} disabled={retry.isPending}>
                    重试
                  </button>
                ) : null}
              </li>
            ))}
          </ul>
        )}
      </section>
    </>
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

import type { ReactNode } from "react";
import { useState } from "react";
import { useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";
import { Loader2 } from "lucide-react";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { apiRequest } from "../../lib/api/client";
import { toApiError } from "../../lib/api/errors";
import {
  repositoriesQueryOptions,
  updateRepositorySettings,
  type Repository,
  type RepositorySettings,
} from "./api";

interface Page<T> {
  items: T[];
  total: number;
}

interface WorkItem {
  id: string;
  kind: string;
  number: number;
  title: string;
  state: string;
  html_url: string;
  author: string;
  repository_full_name?: string;
}

interface WorkflowRun {
  id: string;
  workflow_name: string;
  run_number: number;
  head_branch: string;
  conclusion?: string | null;
  status: string;
  html_url: string;
  repository_full_name?: string;
}

interface SecurityAlert {
  id: string;
  alert_kind: string;
  alert_number: number;
  state: string;
  severity: string;
  rule_or_dependency: string;
  html_url: string;
  repository_full_name?: string;
}

function LoadingIndicator() {
  return (
    <div className="loading-indicator">
      <Loader2 size={20} className="spin" aria-hidden="true" />
      <span>加载中…</span>
    </div>
  );
}

function WorkItemsList({ kind, title, description }: { kind: string; title: string; description: string }) {
  const [state, setState] = useState<string>("open");
  const q = useQuery({
    queryKey: ["work-items", kind, state],
    queryFn: () => {
      const params = new URLSearchParams({ per_page: "50", kind });
      if (state) params.set("state", state);
      return apiRequest<Page<WorkItem>>(`/api/v1/work-items?${params.toString()}`);
    },
  });
  return (
    <ListShell eyebrow="仓库" title={title} description={description}>
      <div className="filter-bar">
        <button className={`quiet-button${state === "" ? " active" : ""}`} type="button" onClick={() => setState("")}>全部</button>
        <button className={`quiet-button${state === "open" ? " active" : ""}`} type="button" onClick={() => setState("open")}>Open</button>
        <button className={`quiet-button${state === "closed" ? " active" : ""}`} type="button" onClick={() => setState("closed")}>Closed</button>
      </div>
      {q.isLoading ? <LoadingIndicator /> : q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((it) => {
            const num = it.number ?? 0;
            const itemTitle = (it.title || "").trim() || "（无标题）";
            return (
              <li key={it.id}>
                <span className={`event-kind state-${it.state || "open"}`}>{it.state || "—"}</span>
                {it.repository_full_name ? <span className="event-repo">{it.repository_full_name}</span> : null}
                <strong>#{num} {itemTitle}</strong>
                <span className="muted">{it.author ? ` · ${it.author}` : ""}</span>
                {it.html_url ? (
                  <a className="quiet-button" href={it.html_url} target="_blank" rel="noreferrer">打开</a>
                ) : null}
              </li>
            );
          })}
        </ul>
      ) : (
        <EmptyState
          title={state === "closed" ? "没有已关闭的项目" : "暂无工作项"}
          description={state === "closed" ? "已关闭的 Issues 或 PR 会显示在这里。" : "先完成 GitHub App 安装，再在仪表盘对仓库点「对账」或等待 Webhook。"}
          action={<Link to="/">回仪表盘对账</Link>}
        />
      )}
    </ListShell>
  );
}

export function IssuesPage() {
  return <WorkItemsList kind="issue" title="Issues" description="自有仓与外部公开仓的 Issue 列表。默认显示 Open。" />;
}

export function PullRequestsPage() {
  return <WorkItemsList kind="pull_request" title="Pull Requests" description="自有仓与外部公开仓的 PR 列表。默认显示 Open。" />;
}

export function ReposPage() {
  const queryClient = useQueryClient();
  const repos = useQuery(repositoriesQueryOptions);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [savingId, setSavingId] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);

  const updateSettings = useMutation({
    mutationFn: ({ id, settings }: { id: string; settings: RepositorySettings }) => {
      setErrorMsg(null);
      setSavingId(id);
      return updateRepositorySettings(id, settings);
    },
    onMutate: async ({ id, settings }) => {
      await queryClient.cancelQueries({ queryKey: ["repositories"] });
      const prev = queryClient.getQueryData<{ items: Repository[] }>(["repositories"]);
      queryClient.setQueryData<{ items: Repository[] }>(["repositories"], (old) => {
        if (!old) return old;
        return {
          ...old,
          items: old.items.map((r) => {
            if (r.id !== id) return r;
            const updated = { ...r, ...settings };
            if (settings.is_archived === true) {
              updated.monitor_enabled = false;
              updated.issues_enabled = false;
              updated.pr_enabled = false;
              updated.actions_enabled = false;
              updated.alerts_enabled = false;
            }
            if (settings.is_archived === false) {
              updated.monitor_enabled = true;
              updated.issues_enabled = true;
              updated.pr_enabled = true;
              updated.actions_enabled = true;
              updated.alerts_enabled = true;
            }
            return updated;
          }),
        };
      });
      return { prev };
    },
    onError: (error, _vars, context) => {
      setSavingId(null);
      setErrorMsg(toApiError(error).message || "保存失败");
      if (context?.prev) {
        queryClient.setQueryData(["repositories"], context.prev);
      }
    },
    onSuccess: async () => {
      setSavingId(null);
      await queryClient.invalidateQueries({ queryKey: ["dashboard"] });
    },
  });

  const allRepos = repos.data?.items ?? [];
  const activeRepos = allRepos.filter((r) => !r.is_archived);
  const archivedRepos = allRepos.filter((r) => r.is_archived);
  const displayed = showArchived ? archivedRepos : activeRepos;

  return (
    <ListShell eyebrow="仓库" title="仓库管理" description="管理仓库监控开关、能力开关和归档状态。">
      {errorMsg ? <ErrorAlert title="更新失败" message={errorMsg} /> : null}
      <div className="filter-bar">
        <button className={`quiet-button${!showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(false)}>
          关注中 ({activeRepos.length})
        </button>
        <button className={`quiet-button${showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(true)}>
          已归档 ({archivedRepos.length})
        </button>
      </div>
      {repos.isLoading ? <LoadingIndicator /> : displayed.length ? (
        <ul className="repo-settings-list">
          {displayed.map((repo) => (
            <RepoCard key={repo.id} repo={repo} onToggle={(settings) => updateSettings.mutate({ id: repo.id, settings })} saving={savingId === repo.id} />
          ))}
        </ul>
      ) : (
        <EmptyState
          title={showArchived ? "没有已归档的仓库" : "暂无关注的仓库"}
          description={showArchived ? "归档的仓库会显示在这里。" : "安装 GitHub App 后仓库会自动出现。"}
          action={showArchived ? undefined : <Link to="/github">打开 GitHub App 页</Link>}
        />
      )}
    </ListShell>
  );
}

function RepoCard({ repo, onToggle, saving }: { repo: Repository; onToggle: (s: RepositorySettings) => void; saving: boolean }) {
  return (
    <li className="repo-card">
      <div className="repo-card__header">
        <strong>{repo.full_name || `${repo.owner}/${repo.name}`}</strong>
        <span className="muted">
          {repo.sync_status === "active" ? "正常" : repo.sync_status === "archived" ? "已归档" : repo.sync_status === "baseline_sync" ? "基线中" : repo.sync_status}
          {repo.is_archived ? " · GitHub 已归档" : ""}
        </span>
      </div>
      <div className="repo-card__toggles">
        <Toggle label="监控" checked={repo.monitor_enabled} disabled={saving} onChange={(v) => onToggle({ monitor_enabled: v })} />
        <Toggle label="Issues" checked={repo.issues_enabled} disabled={saving} onChange={(v) => onToggle({ issues_enabled: v })} />
        <Toggle label="PR" checked={repo.pr_enabled} disabled={saving} onChange={(v) => onToggle({ pr_enabled: v })} />
        <Toggle label="Actions" checked={repo.actions_enabled} disabled={saving} onChange={(v) => onToggle({ actions_enabled: v })} />
        <Toggle label="安全告警" checked={repo.alerts_enabled} disabled={saving} onChange={(v) => onToggle({ alerts_enabled: v })} />
        <Toggle label="归档" checked={repo.is_archived} disabled={saving} onChange={(v) => onToggle({ is_archived: v })} />
      </div>
    </li>
  );
}

function Toggle({ label, checked, disabled, onChange }: { label: string; checked: boolean; disabled: boolean; onChange: (v: boolean) => void }) {
  return (
    <label className="toggle-row">
      <input type="checkbox" checked={checked} disabled={disabled} aria-label={label} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  );
}

export function ActionsPage() {
  const q = useQuery({
    queryKey: ["workflow-runs"],
    queryFn: () => apiRequest<Page<WorkflowRun>>("/api/v1/workflow-runs?per_page=50"),
  });
  return (
    <ListShell eyebrow="仓库" title="Actions" description="Workflow Run 结论与恢复状态。">
      {q.isLoading ? <LoadingIndicator /> : q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((run) => {
            const name = (run.workflow_name || "").trim() || "workflow";
            const num = run.run_number ?? 0;
            const conclusion = run.conclusion || run.status || "run";
            return (
              <li key={run.id}>
                <span className={`event-kind state-${conclusion}`}>{conclusion}</span>
                {run.repository_full_name ? <span className="event-repo">{run.repository_full_name}</span> : null}
                <strong>{name} #{num}</strong>
                <span className="muted">{run.head_branch || "—"}</span>
                {run.html_url ? <a className="quiet-button" href={run.html_url} target="_blank" rel="noreferrer">打开</a> : null}
              </li>
            );
          })}
        </ul>
      ) : (
        <EmptyState title="暂无 Actions 运行" description="确认 App 有 Actions 只读权限并订阅了 workflow_run。" action={<Link to="/">回仪表盘对账</Link>} />
      )}
    </ListShell>
  );
}

export function SecurityPage() {
  const [state, setState] = useState<string>("open");
  const [alertKind, setAlertKind] = useState<string>("");
  const q = useQuery({
    queryKey: ["security-alerts", state, alertKind],
    queryFn: () => {
      const params = new URLSearchParams({ per_page: "50" });
      if (state) params.set("state", state);
      if (alertKind) params.set("alert_kind", alertKind);
      return apiRequest<Page<SecurityAlert>>(`/api/v1/security-alerts?${params.toString()}`);
    },
  });
  return (
    <ListShell eyebrow="仓库" title="安全告警" description="Dependabot / Code Scanning / Secret Scanning。">
      <div className="filter-bar">
        <button className={`quiet-button${state === "" ? " active" : ""}`} type="button" onClick={() => setState("")}>全部</button>
        <button className={`quiet-button${state === "open" ? " active" : ""}`} type="button" onClick={() => setState("open")}>Open</button>
        <button className={`quiet-button${state === "dismissed" ? " active" : ""}`} type="button" onClick={() => setState("dismissed")}>Dismissed</button>
        <span className="filter-bar__sep" />
        <button className={`quiet-button${alertKind === "" ? " active" : ""}`} type="button" onClick={() => setAlertKind("")}>全部类型</button>
        <button className={`quiet-button${alertKind === "dependabot" ? " active" : ""}`} type="button" onClick={() => setAlertKind("dependabot")}>Dependabot</button>
        <button className={`quiet-button${alertKind === "code_scanning" ? " active" : ""}`} type="button" onClick={() => setAlertKind("code_scanning")}>Code Scanning</button>
        <button className={`quiet-button${alertKind === "secret_scanning" ? " active" : ""}`} type="button" onClick={() => setAlertKind("secret_scanning")}>Secret Scanning</button>
      </div>
      {q.isLoading ? <LoadingIndicator /> : q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((a) => {
            const num = a.alert_number ?? 0;
            const label = (a.rule_or_dependency || a.severity || "").trim() || "告警";
            return (
              <li key={a.id}>
                <span className={`event-kind state-${a.state || "open"}`}>{a.alert_kind || "alert"}</span>
                {a.repository_full_name ? <span className="event-repo">{a.repository_full_name}</span> : null}
                <strong>#{num} {label}</strong>
                <span className="muted">{a.state || "—"}{a.severity ? ` · ${a.severity}` : ""}</span>
                {a.html_url ? <a className="quiet-button" href={a.html_url} target="_blank" rel="noreferrer">打开</a> : null}
              </li>
            );
          })}
        </ul>
      ) : (
        <EmptyState
          title={state === "dismissed" ? "没有已忽略的告警" : "暂无安全告警"}
          description={state === "dismissed" ? "已忽略的告警会显示在这里。" : "在 GitHub 开启仓库安全功能后，Webhook 或对账会写入此处。"}
          action={<Link to="/github">查看权限清单</Link>}
        />
      )}
    </ListShell>
  );
}

export function ListShell({ eyebrow = "仓库值守", title, description, children }: { eyebrow?: string; title: string; description: string; children: ReactNode }) {
  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">{eyebrow}</p>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
      </section>
      <section className="onboarding-card">{children}</section>
    </>
  );
}

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
  // 新增字段：展示后端已有但前端未展示的数据
  draft?: boolean;
  merged?: boolean;
  labels?: string[];
  assignees?: string[];
  milestone?: string;
  source_updated_at?: string;
  // 新增 Review 相关字段
  review_state?: string;
  review_decision?: string;
  reviewers?: string[];
  // 新增 Check Runs 相关字段
  check_status?: string;
  check_conclusion?: string;
  checks_total?: number;
  checks_passed?: number;
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
  // 新增字段：展示后端已有但前端未展示的数据
  actor?: string;
  event?: string;
  run_attempt?: number;
  run_started_at?: string;
  run_completed_at?: string;
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

// 格式化相对时间
function formatRelativeTime(dateString: string): string {
  if (!dateString) return '';
  const date = new Date(dateString);
  if (isNaN(date.getTime())) return '';
  const now = new Date();
  const diffMs = now.getTime() - date.getTime();
  // 未来时间不显示
  if (diffMs < 0) return '';
  const diffSeconds = Math.floor(diffMs / 1000);
  const diffMinutes = Math.floor(diffSeconds / 60);
  const diffHours = Math.floor(diffMinutes / 60);
  const diffDays = Math.floor(diffHours / 24);

  if (diffSeconds < 60) return '刚刚';
  if (diffMinutes < 60) return `${diffMinutes} 分钟前`;
  if (diffHours < 24) return `${diffHours} 小时前`;
  if (diffDays < 30) return `${diffDays} 天前`;
  return date.toLocaleDateString('zh-CN');
}

function WorkItemsList({ kind, title, description }: { kind: string; title: string; description: string }) {
  const [state, setState] = useState<string>("open");
  const [reviewFilter, setReviewFilter] = useState<string>("");
  const [checkFilter, setCheckFilter] = useState<string>("");
  const q = useQuery({
    queryKey: ["work-items", kind, state],
    queryFn: () => {
      const params = new URLSearchParams({ per_page: "50", kind });
      if (state) params.set("state", state);
      return apiRequest<Page<WorkItem>>(`/api/v1/work-items?${params.toString()}`);
    },
  });

  // 客户端筛选
  const filteredItems = (q.data?.items || []).filter((it) => {
    // 审核状态筛选
    if (reviewFilter === "approved" && it.review_decision !== "approved") return false;
    if (reviewFilter === "changes_requested" && it.review_decision !== "changes_requested") return false;
    if (reviewFilter === "pending" && it.review_decision) return false;

    // 检查状态筛选
    if (checkFilter === "passed" && it.check_status !== "success") return false;
    if (checkFilter === "failed" && it.check_status !== "failure") return false;
    if (checkFilter === "pending" && it.check_status) return false;

    return true;
  });

  return (
    <ListShell eyebrow="仓库" title={title} description={description}>
      <div className="filter-bar">
        <button className={`quiet-button${state === "" ? " active" : ""}`} type="button" onClick={() => setState("")}>全部</button>
        <button className={`quiet-button${state === "open" ? " active" : ""}`} type="button" onClick={() => setState("open")}>Open</button>
        <button className={`quiet-button${state === "closed" ? " active" : ""}`} type="button" onClick={() => setState("closed")}>Closed</button>
        {kind === "pull_request" && (
          <>
            <span className="filter-bar__sep" />
            <button className={`quiet-button${reviewFilter === "" ? " active" : ""}`} type="button" onClick={() => setReviewFilter("")}>全部审核</button>
            <button className={`quiet-button${reviewFilter === "approved" ? " active" : ""}`} type="button" onClick={() => setReviewFilter("approved")}>Approved</button>
            <button className={`quiet-button${reviewFilter === "changes_requested" ? " active" : ""}`} type="button" onClick={() => setReviewFilter("changes_requested")}>Changes Requested</button>
            <button className={`quiet-button${reviewFilter === "pending" ? " active" : ""}`} type="button" onClick={() => setReviewFilter("pending")}>Pending Review</button>
            <span className="filter-bar__sep" />
            <button className={`quiet-button${checkFilter === "" ? " active" : ""}`} type="button" onClick={() => setCheckFilter("")}>全部检查</button>
            <button className={`quiet-button${checkFilter === "passed" ? " active" : ""}`} type="button" onClick={() => setCheckFilter("passed")}>Checks Passed</button>
            <button className={`quiet-button${checkFilter === "failed" ? " active" : ""}`} type="button" onClick={() => setCheckFilter("failed")}>Checks Failed</button>
            <button className={`quiet-button${checkFilter === "pending" ? " active" : ""}`} type="button" onClick={() => setCheckFilter("pending")}>Checks Pending</button>
          </>
        )}
      </div>
      {q.isLoading ? <LoadingIndicator /> : filteredItems.length ? (
        <ul className="event-list">
          {filteredItems.map((it) => {
            const num = it.number ?? 0;
            const itemTitle = (it.title || "").trim() || "（无标题）";
            return (
              <li key={it.id}>
                <div className="pr-header">
                  <span className={`event-kind state-${it.state || "open"}`}>{it.state || "—"}</span>
                  {it.draft && <span className="draft-badge">Draft</span>}
                  {it.merged && <span className="merged-badge">Merged</span>}
                  {it.repository_full_name ? <span className="event-repo">{it.repository_full_name}</span> : null}
                  <strong>#{num} {itemTitle}</strong>
                </div>
                <div className="pr-meta">
                  <span className="muted">{it.author ? ` · ${it.author}` : ""}</span>
                  {it.labels && it.labels.length > 0 && (
                    <div className="labels">
                      {it.labels.slice(0, 3).map((label, idx) => (
                        <span key={idx} className="label">{label}</span>
                      ))}
                      {it.labels.length > 3 && <span className="label-more">+{it.labels.length - 3}</span>}
                    </div>
                  )}
                  {it.assignees && it.assignees.length > 0 && (
                    <span className="assignees">→ {it.assignees.join(', ')}</span>
                  )}
                  {it.milestone && <span className="milestone">🎯 {it.milestone}</span>}
                  {it.source_updated_at && (
                    <span className="updated-at">{formatRelativeTime(it.source_updated_at)}</span>
                  )}
                </div>
                <div className="pr-status">
                  {/* Review 状态 */}
                  {it.review_decision ? (
                    <span className={`review-badge review-${it.review_decision}`}>
                      {it.review_decision === 'approved' ? '✅ Approved' :
                       it.review_decision === 'changes_requested' ? '❌ Changes Requested' :
                       it.review_state || 'Pending'}
                    </span>
                  ) : it.reviewers && it.reviewers.length > 0 ? (
                    <span className="review-badge review-pending">⏳ Pending Review</span>
                  ) : null}
                  {it.reviewers && it.reviewers.length > 0 && (
                    <span className="reviewers">👀 {it.reviewers.join(', ')}</span>
                  )}
                  {/* Check 状态 */}
                  {it.checks_total && it.checks_total > 0 && (
                    <span className={`check-badge check-${it.check_status || 'pending'}`}>
                      {it.check_status === 'success' ? '✅' :
                       it.check_status === 'failure' ? '❌' : '⏳'}
                      {it.checks_passed}/{it.checks_total} checks
                    </span>
                  )}
                </div>
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
        <div className="repo-card__title">
          <strong>{repo.full_name || `${repo.owner}/${repo.name}`}</strong>
          {repo.is_private && <span className="private-badge">🔒</span>}
          {repo.type === "external_public" && <span className="type-badge">外部</span>}
          {repo.default_branch && <span className="branch-badge">{repo.default_branch}</span>}
        </div>
        <div className="repo-card__status">
          <span className="muted">
            {repo.sync_status === "active" ? "正常" : repo.sync_status === "archived" ? "已归档" : repo.sync_status === "baseline_sync" ? "基线中" : repo.sync_status}
            {repo.is_archived ? " · GitHub 已归档" : ""}
          </span>
          {repo.last_synced_at && (
            <span className="last-synced">最后同步: {formatRelativeTime(repo.last_synced_at)}</span>
          )}
          {repo.last_sync_error_code && (
            <span className="sync-error">同步错误: {repo.last_sync_error_code}</span>
          )}
        </div>
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
                <div className="run-header">
                  <span className={`event-kind state-${conclusion}`}>{conclusion}</span>
                  {run.event && <span className="event-type">{run.event}</span>}
                  {run.run_attempt && run.run_attempt > 1 && (
                    <span className="attempt-badge">Attempt #{run.run_attempt}</span>
                  )}
                  {run.repository_full_name ? <span className="event-repo">{run.repository_full_name}</span> : null}
                  <strong>{name} #{num}</strong>
                  <span className="muted">{run.head_branch || "—"}</span>
                </div>
                <div className="run-meta">
                  {run.actor && <span className="actor">by {run.actor}</span>}
                  {run.run_started_at && (
                    <span className="started-at">{formatRelativeTime(run.run_started_at)}</span>
                  )}
                </div>
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

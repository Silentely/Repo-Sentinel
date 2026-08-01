import { useState } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate } from "../../components/query-gate";
import { apiRequest } from "../../lib/api/client";
import { toApiError } from "../../lib/api/errors";
import { formatRelativeTime } from "../../lib/format";
import {
  type Page,
  repositoriesQueryOptions,
  setSecurityAlertIgnored,
  setWorkItemIgnored,
  setWorkflowRunIgnored,
  settingsQueryOptions,
  updateRepositorySettings,
  type Repository,
  type RepositorySettings,
} from "./api";
import {
  FeatureGuard,
  IgnoreButton,
  IgnoredToggle,
  ListShell,
  RepoFilterSelect,
  useActiveRepos,
  useIgnoreMutation,
  type IgnoredMode,
} from "./list-shared";

interface WorkItem {
  id: string;
  kind: string;
  number: number;
  title: string;
  state: string;
  html_url: string;
  author: string;
  repository_id?: string;
  repository_full_name?: string;
  draft?: boolean;
  merged?: boolean;
  labels?: string[];
  assignees?: string[];
  milestone?: string;
  source_updated_at?: string;
  review_state?: string;
  review_decision?: string;
  reviewers?: string[];
  check_status?: string;
  check_conclusion?: string;
  checks_total?: number;
  checks_passed?: number;
  ignored?: boolean;
}

interface WorkflowRun {
  id: string;
  workflow_name: string;
  run_number: number;
  head_branch: string;
  conclusion?: string | null;
  status: string;
  html_url: string;
  repository_id?: string;
  repository_full_name?: string;
  actor?: string;
  event?: string;
  run_attempt?: number;
  run_started_at?: string;
  run_completed_at?: string;
  ignored?: boolean;
}

interface SecurityAlert {
  id: string;
  alert_kind: string;
  alert_number: number;
  state: string;
  severity: string;
  rule_or_dependency: string;
  html_url: string;
  repository_id?: string;
  repository_full_name?: string;
  ignored?: boolean;
}

function WorkItemsList({ kind, title, description }: { kind: string; title: string; description: string }) {
  const { active: activeRepos } = useActiveRepos();
  const [state, setState] = useState<string>("open");
  const [repoId, setRepoId] = useState<string>("");
  const [ignoredMode, setIgnoredMode] = useState<IgnoredMode>("active");
  const [reviewFilter, setReviewFilter] = useState<string>("");
  const [checkFilter, setCheckFilter] = useState<string>("");

  // 审核/检查状态由后端按 review/check 参数过滤（total 为过滤后总数），客户端不再二次过滤；
  // 每页 50 条，超过时通过「加载更多」翻页拉取。
  const q = useInfiniteQuery({
    queryKey: ["work-items", kind, state, repoId, ignoredMode, reviewFilter, checkFilter],
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ per_page: "50", kind, page: String(pageParam) });
      if (state) params.set("state", state);
      if (repoId) params.set("repository_id", repoId);
      if (ignoredMode === "ignored") params.set("ignored", "true");
      if (reviewFilter) params.set("review", reviewFilter);
      if (checkFilter) params.set("check", checkFilter);
      return apiRequest<Page<WorkItem>>(`/api/v1/work-items?${params.toString()}`);
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page * lastPage.per_page < lastPage.total ? lastPage.page + 1 : undefined,
  });

  const { mutation: ignoreMutation, busyId } = useIgnoreMutation(setWorkItemIgnored, ["work-items"]);

  const items = q.data?.pages.flatMap((page) => page.items) ?? [];
  const total = q.data?.pages[q.data.pages.length - 1]?.total ?? 0;
  // 仓库/审核/检查筛选激活时，空态需区分「筛选后为空」与「真的没有」。
  const filtersActive = repoId !== "" || reviewFilter !== "" || checkFilter !== "";

  return (
    <ListShell eyebrow="仓库" title={title} description={description}>
      <div className="filter-bar filter-bar--wrap">
        <button className={`quiet-button${state === "" ? " active" : ""}`} type="button" onClick={() => setState("")}>
          全部
        </button>
        <button className={`quiet-button${state === "open" ? " active" : ""}`} type="button" onClick={() => setState("open")}>
          进行中
        </button>
        <button className={`quiet-button${state === "closed" ? " active" : ""}`} type="button" onClick={() => setState("closed")}>
          已关闭
        </button>
        <IgnoredToggle mode={ignoredMode} onChange={setIgnoredMode} />
        <span className="filter-bar__sep" />
        <RepoFilterSelect value={repoId} onChange={setRepoId} repos={activeRepos} />
        {kind === "pull_request" && (
          <>
            <span className="filter-bar__sep" />
            <label className="repo-filter">
              <span className="sr-only">审核状态</span>
              <select value={reviewFilter} onChange={(e) => setReviewFilter(e.target.value)} aria-label="审核状态">
                <option value="">全部审核</option>
                <option value="approved">已通过</option>
                <option value="changes_requested">需修改</option>
                <option value="pending">待审核</option>
              </select>
            </label>
            <label className="repo-filter">
              <span className="sr-only">检查状态</span>
              <select value={checkFilter} onChange={(e) => setCheckFilter(e.target.value)} aria-label="检查状态">
                <option value="">全部检查</option>
                <option value="passed">已通过</option>
                <option value="failed">未通过</option>
                <option value="pending">进行中</option>
              </select>
            </label>
          </>
        )}
      </div>
      <QueryGate
        query={q}
        isEmpty={items.length === 0}
        emptyState={
          <EmptyState
            title={
              filtersActive
                ? "没有符合筛选条件的项目"
                : ignoredMode === "ignored"
                  ? "没有已忽略的项目"
                  : state === "closed"
                    ? "没有已关闭的项目"
                    : "暂无工作项"
            }
            description={
              filtersActive
                ? "可尝试调整或清除筛选条件后重试。"
                : ignoredMode === "ignored"
                  ? "忽略的长期打开 Issue/PR 会显示在这里，可随时取消忽略。"
                  : state === "closed"
                    ? "已关闭的 Issues 或 PR 会显示在这里。"
                    : "安装 GitHub App 并完成对账后，相关数据会自动同步到这里。已归档仓库的历史项默认不显示。"
            }
            action={<Link to="/">返回仪表盘</Link>}
          />
        }
      >
        <>
          <ul className="event-list">
            {items.map((it) => {
              const num = it.number ?? 0;
              const itemTitle = (it.title || "").trim() || "（无标题）";
              return (
                <li key={it.id}>
                  <div className="pr-header">
                    <span className={`event-kind state-${it.state || "open"}`}>{it.state || "—"}</span>
                    {it.draft && <span className="draft-badge">Draft</span>}
                    {it.merged && <span className="merged-badge">Merged</span>}
                    {it.ignored && <span className="ignored-badge">已忽略</span>}
                    {it.repository_full_name ? <span className="event-repo">{it.repository_full_name}</span> : null}
                    <strong>
                      #{num} {itemTitle}
                    </strong>
                  </div>
                  <div className="pr-meta">
                    <span className="muted">{it.author ? ` · ${it.author}` : ""}</span>
                    {it.labels && it.labels.length > 0 && (
                      <div className="labels">
                        {it.labels.slice(0, 3).map((label, idx) => (
                          <span key={idx} className="label">
                            {label}
                          </span>
                        ))}
                        {it.labels.length > 3 && <span className="label-more">+{it.labels.length - 3}</span>}
                      </div>
                    )}
                    {it.assignees && it.assignees.length > 0 && (
                      <span className="assignees">→ {it.assignees.join(", ")}</span>
                    )}
                    {it.milestone && <span className="milestone">🎯 {it.milestone}</span>}
                    {it.source_updated_at && <span className="updated-at">{formatRelativeTime(it.source_updated_at)}</span>}
                  </div>
                  <div className="pr-status">
                    {it.review_decision ? (
                      <span className={`review-badge review-${it.review_decision}`}>
                        {it.review_decision === "approved"
                          ? "✅ 审核通过"
                          : it.review_decision === "changes_requested"
                            ? "❌ 需要修改"
                            : it.review_state || "审核中"}
                      </span>
                    ) : it.reviewers && it.reviewers.length > 0 ? (
                      <span className="review-badge review-pending">⏳ 等待审核</span>
                    ) : null}
                    {it.reviewers && it.reviewers.length > 0 && (
                      <span className="reviewers">👀 {it.reviewers.join(", ")}</span>
                    )}
                    {it.checks_total && it.checks_total > 0 && (
                      <span className={`check-badge check-${it.check_status || "pending"}`}>
                        {it.check_status === "success" ? "✅" : it.check_status === "failure" ? "❌" : "⏳"}
                        {it.checks_passed}/{it.checks_total} 项检查通过
                      </span>
                    )}
                  </div>
                  <div className="item-actions">
                    {it.html_url ? (
                      <a className="quiet-button" href={it.html_url} target="_blank" rel="noreferrer">
                        在 GitHub 查看
                      </a>
                    ) : null}
                    <IgnoreButton
                      ignored={it.ignored || ignoredMode === "ignored"}
                      busy={busyId === it.id}
                      onToggle={() =>
                        ignoreMutation.mutate({
                          id: it.id,
                          ignored: !(it.ignored || ignoredMode === "ignored"),
                        })
                      }
                    />
                  </div>
                </li>
              );
            })}
          </ul>
          <ListFooter
            shown={items.length}
            total={total}
            hasNextPage={q.hasNextPage}
            fetchingNextPage={q.isFetchingNextPage}
            onLoadMore={() => q.fetchNextPage()}
          />
        </>
      </QueryGate>
    </ListShell>
  );
}

export function IssuesPage() {
  return (
    <FeatureGuard featureKey="feature.issues" featureName="Issues">
      <WorkItemsList
        kind="issue"
        title="Issues"
        description="自有仓与外部公开仓的 Issue 列表。默认显示 Open，已归档仓库与已忽略项不计入。"
      />
    </FeatureGuard>
  );
}

export function PullRequestsPage() {
  return (
    <FeatureGuard featureKey="feature.pull_requests" featureName="Pull Requests">
      <WorkItemsList
        kind="pull_request"
        title="Pull Requests"
        description="自有仓与外部公开仓的 PR 列表。默认显示 Open，已归档仓库与已忽略项不计入。"
      />
    </FeatureGuard>
  );
}

export function ReposPage() {
  const queryClient = useQueryClient();
  const repos = useQuery(repositoriesQueryOptions);
  const settings = useQuery(settingsQueryOptions);
  const [errorMsg, setErrorMsg] = useState<string | null>(null);
  const [savingId, setSavingId] = useState<string | null>(null);
  const [showArchived, setShowArchived] = useState(false);
  const featureIssues = settings.data?.["feature.issues"] !== false;
  const featurePRs = settings.data?.["feature.pull_requests"] !== false;
  const featureActions = settings.data?.["feature.actions"] !== false;
  const featureAlerts = settings.data?.["feature.security_alerts"] !== false;

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
      await queryClient.invalidateQueries({ queryKey: ["work-items"] });
      await queryClient.invalidateQueries({ queryKey: ["workflow-runs"] });
      await queryClient.invalidateQueries({ queryKey: ["security-alerts"] });
    },
  });

  const allRepos = repos.data?.items ?? [];
  const activeRepos = allRepos.filter((r) => !r.is_archived);
  const archivedRepos = allRepos.filter((r) => r.is_archived);
  const displayed = showArchived ? archivedRepos : activeRepos;

  return (
    <ListShell eyebrow="仓库" title="仓库管理" description="管理仓库监控开关、能力开关和归档状态。全局功能模块关闭时，对应能力在此页会显示为关闭且不可改；重新开启全局后恢复各仓原有配置。">
      {errorMsg ? <ErrorAlert title="更新失败" message={errorMsg} /> : null}
      <div className="filter-bar">
        <button className={`quiet-button${!showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(false)}>
          关注中 ({activeRepos.length})
        </button>
        <button className={`quiet-button${showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(true)}>
          已归档 ({archivedRepos.length})
        </button>
      </div>
      <QueryGate
        query={repos}
        errorTitle="无法加载仓库列表"
        isEmpty={displayed.length === 0}
        emptyState={
          <EmptyState
            title={showArchived ? "没有已归档的仓库" : "暂无关注的仓库"}
            description={showArchived ? "归档的仓库会显示在这里。" : "安装 GitHub App 后仓库会自动出现。"}
            action={showArchived ? undefined : <Link to="/github">打开 GitHub App 页</Link>}
          />
        }
      >
        <ul className="repo-settings-list">
          {displayed.map((repo) => (
            <RepoCard
              key={repo.id}
              repo={repo}
              onToggle={(settings) => updateSettings.mutate({ id: repo.id, settings })}
              saving={savingId === repo.id}
              features={{
                issues: featureIssues,
                prs: featurePRs,
                actions: featureActions,
                alerts: featureAlerts,
              }}
            />
          ))}
        </ul>
      </QueryGate>
    </ListShell>
  );
}

function RepoCard({
  repo,
  onToggle,
  saving,
  features,
}: {
  repo: Repository;
  onToggle: (s: RepositorySettings) => void;
  saving: boolean;
  features: { issues: boolean; prs: boolean; actions: boolean; alerts: boolean };
}) {
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
            {repo.sync_status === "active"
              ? "正常"
              : repo.sync_status === "archived"
                ? "已归档"
                : repo.sync_status === "baseline_sync"
                  ? "基线中"
                  : repo.sync_status}
            {repo.is_archived ? " · GitHub 已归档" : ""}
          </span>
          {repo.last_synced_at && <span className="last-synced">最后同步: {formatRelativeTime(repo.last_synced_at)}</span>}
          {repo.last_sync_error_code && <span className="sync-error">同步错误: {repo.last_sync_error_code}</span>}
        </div>
      </div>
      <div className="repo-card__toggles">
        <Toggle label="监控" checked={repo.monitor_enabled} disabled={saving} onChange={(v) => onToggle({ monitor_enabled: v })} />
        <Toggle
          label="Issues"
          checked={features.issues && repo.issues_enabled}
          disabled={saving || !features.issues}
          onChange={(v) => onToggle({ issues_enabled: v })}
        />
        <Toggle
          label="PR"
          checked={features.prs && repo.pr_enabled}
          disabled={saving || !features.prs}
          onChange={(v) => onToggle({ pr_enabled: v })}
        />
        <Toggle
          label="Actions"
          checked={features.actions && repo.actions_enabled}
          disabled={saving || !features.actions}
          onChange={(v) => onToggle({ actions_enabled: v })}
        />
        <Toggle
          label="安全告警"
          checked={features.alerts && repo.alerts_enabled}
          disabled={saving || !features.alerts}
          onChange={(v) => onToggle({ alerts_enabled: v })}
        />
        <Toggle label="归档" checked={repo.is_archived} disabled={saving} onChange={(v) => onToggle({ is_archived: v })} />
      </div>
    </li>
  );
}

function Toggle({
  label,
  checked,
  disabled,
  onChange,
}: {
  label: string;
  checked: boolean;
  disabled: boolean;
  onChange: (v: boolean) => void;
}) {
  return (
    <label className="toggle-row">
      <input type="checkbox" checked={checked} disabled={disabled} aria-label={label} onChange={(e) => onChange(e.target.checked)} />
      <span>{label}</span>
    </label>
  );
}

function ActionsList() {
  const { active: activeRepos } = useActiveRepos();
  const [repoId, setRepoId] = useState<string>("");
  const [conclusion, setConclusion] = useState<string>("");
  const [ignoredMode, setIgnoredMode] = useState<IgnoredMode>("active");
  const { mutation: ignoreMutation, busyId } = useIgnoreMutation(setWorkflowRunIgnored, ["workflow-runs"]);

  const q = useInfiniteQuery({
    queryKey: ["workflow-runs", repoId, conclusion, ignoredMode],
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ per_page: "50", page: String(pageParam) });
      if (repoId) params.set("repository_id", repoId);
      if (conclusion) params.set("conclusion", conclusion);
      if (ignoredMode === "ignored") params.set("ignored", "true");
      return apiRequest<Page<WorkflowRun>>(`/api/v1/workflow-runs?${params.toString()}`);
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page * lastPage.per_page < lastPage.total ? lastPage.page + 1 : undefined,
  });

  const items = q.data?.pages.flatMap((page) => page.items) ?? [];
  const total = q.data?.pages[q.data.pages.length - 1]?.total ?? 0;
  const filtersActive = repoId !== "" || conclusion !== "";

  return (
    <ListShell
      eyebrow="仓库"
      title="Actions"
      description="Workflow Run 结论与恢复状态。依赖 GitHub App 的 Actions 只读权限与 workflow_run 事件；也可在仪表盘触发对账补拉。"
    >
      <div className="filter-bar filter-bar--wrap">
        <button className={`quiet-button${conclusion === "" ? " active" : ""}`} type="button" onClick={() => setConclusion("")}>
          全部
        </button>
        <button
          className={`quiet-button${conclusion === "failure" ? " active" : ""}`}
          type="button"
          onClick={() => setConclusion("failure")}
        >
          失败
        </button>
        <button
          className={`quiet-button${conclusion === "success" ? " active" : ""}`}
          type="button"
          onClick={() => setConclusion("success")}
        >
          成功
        </button>
        <button
          className={`quiet-button${conclusion === "cancelled" ? " active" : ""}`}
          type="button"
          onClick={() => setConclusion("cancelled")}
        >
          已取消
        </button>
        <IgnoredToggle mode={ignoredMode} onChange={setIgnoredMode} />
        <span className="filter-bar__sep" />
        <RepoFilterSelect value={repoId} onChange={setRepoId} repos={activeRepos} />
      </div>
      <QueryGate
        query={q}
        isEmpty={items.length === 0}
        emptyState={
          <EmptyState
            title={
              filtersActive
                ? "没有符合筛选条件的运行"
                : ignoredMode === "ignored"
                  ? "没有已忽略的运行"
                  : "暂无 Actions 运行"
            }
            description={
              filtersActive
                ? "可尝试调整或清除筛选条件后重试。"
                : ignoredMode === "ignored"
                  ? "忽略的运行记录会显示在这里。"
                  : "常见原因：① GitHub App 未授予 Actions 只读权限；② 未订阅 workflow_run 事件；③ 仓库尚未对账/基线同步。可到「仓库管理」确认 Actions 开关开启，并在仪表盘触发对账。"
            }
            action={<Link to="/github">检查 GitHub App 权限</Link>}
          />
        }
      >
        <>
          <ul className="event-list">
            {items.map((run) => {
              const name = (run.workflow_name || "").trim() || "workflow";
              const num = run.run_number ?? 0;
              const runConclusion = run.conclusion || run.status || "run";
              return (
                <li key={run.id}>
                  <div className="run-header">
                    <span className={`event-kind state-${runConclusion}`}>{runConclusion}</span>
                    {run.event && <span className="event-type">{run.event}</span>}
                    {run.run_attempt && run.run_attempt > 1 && <span className="attempt-badge">Attempt #{run.run_attempt}</span>}
                    {run.ignored && <span className="ignored-badge">已忽略</span>}
                    {run.repository_full_name ? <span className="event-repo">{run.repository_full_name}</span> : null}
                    <strong>
                      {name} #{num}
                    </strong>
                    <span className="muted">{run.head_branch || "—"}</span>
                  </div>
                  <div className="run-meta">
                    {run.actor && <span className="actor">by {run.actor}</span>}
                    {run.run_started_at && <span className="started-at">{formatRelativeTime(run.run_started_at)}</span>}
                  </div>
                  <div className="item-actions">
                    {run.html_url ? (
                      <a className="quiet-button" href={run.html_url} target="_blank" rel="noreferrer">
                        在 GitHub 查看
                      </a>
                    ) : null}
                    <IgnoreButton
                      ignored={run.ignored || ignoredMode === "ignored"}
                      busy={busyId === run.id}
                      onToggle={() =>
                        ignoreMutation.mutate({
                          id: run.id,
                          ignored: !(run.ignored || ignoredMode === "ignored"),
                        })
                      }
                    />
                  </div>
                </li>
              );
            })}
          </ul>
          <ListFooter
            shown={items.length}
            total={total}
            hasNextPage={q.hasNextPage}
            fetchingNextPage={q.isFetchingNextPage}
            onLoadMore={() => q.fetchNextPage()}
          />
        </>
      </QueryGate>
    </ListShell>
  );
}

export function ActionsPage() {
  return (
    <FeatureGuard
      featureKey="feature.actions"
      featureName="Actions"
      description="Workflow Run 结论与恢复状态。依赖 GitHub App 的 Actions 只读权限与 workflow_run 事件；也可在仪表盘触发对账补拉。"
    >
      <ActionsList />
    </FeatureGuard>
  );
}

function SecurityList() {
  const { active: activeRepos } = useActiveRepos();
  const [state, setState] = useState<string>("open");
  const [alertKind, setAlertKind] = useState<string>("");
  const [repoId, setRepoId] = useState<string>("");
  const [ignoredMode, setIgnoredMode] = useState<IgnoredMode>("active");
  const { mutation: ignoreMutation, busyId } = useIgnoreMutation(setSecurityAlertIgnored, ["security-alerts"]);

  const q = useInfiniteQuery({
    queryKey: ["security-alerts", state, alertKind, repoId, ignoredMode],
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ per_page: "50", page: String(pageParam) });
      if (state) params.set("state", state);
      if (alertKind) params.set("alert_kind", alertKind);
      if (repoId) params.set("repository_id", repoId);
      if (ignoredMode === "ignored") params.set("ignored", "true");
      return apiRequest<Page<SecurityAlert>>(`/api/v1/security-alerts?${params.toString()}`);
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page * lastPage.per_page < lastPage.total ? lastPage.page + 1 : undefined,
  });

  const items = q.data?.pages.flatMap((page) => page.items) ?? [];
  const total = q.data?.pages[q.data.pages.length - 1]?.total ?? 0;
  const filtersActive = repoId !== "" || alertKind !== "";

  return (
    <ListShell eyebrow="仓库" title="安全告警" description="Dependabot / Code Scanning / Secret Scanning 安全告警。">
      <div className="filter-bar filter-bar--wrap">
        <button className={`quiet-button${state === "" ? " active" : ""}`} type="button" onClick={() => setState("")}>
          全部
        </button>
        <button className={`quiet-button${state === "open" ? " active" : ""}`} type="button" onClick={() => setState("open")}>
          待处理
        </button>
        <button
          className={`quiet-button${state === "dismissed" ? " active" : ""}`}
          type="button"
          onClick={() => setState("dismissed")}
        >
          GitHub 已忽略
        </button>
        <IgnoredToggle mode={ignoredMode} onChange={setIgnoredMode} />
        <span className="filter-bar__sep" />
        <button className={`quiet-button${alertKind === "" ? " active" : ""}`} type="button" onClick={() => setAlertKind("")}>
          全部类型
        </button>
        <button
          className={`quiet-button${alertKind === "dependabot" ? " active" : ""}`}
          type="button"
          onClick={() => setAlertKind("dependabot")}
        >
          依赖漏洞
        </button>
        <button
          className={`quiet-button${alertKind === "code_scanning" ? " active" : ""}`}
          type="button"
          onClick={() => setAlertKind("code_scanning")}
        >
          代码扫描
        </button>
        <button
          className={`quiet-button${alertKind === "secret_scanning" ? " active" : ""}`}
          type="button"
          onClick={() => setAlertKind("secret_scanning")}
        >
          密钥泄露
        </button>
        <span className="filter-bar__sep" />
        <RepoFilterSelect value={repoId} onChange={setRepoId} repos={activeRepos} />
      </div>
      <QueryGate
        query={q}
        isEmpty={items.length === 0}
        emptyState={
          <EmptyState
            title={
              filtersActive
                ? "没有符合筛选条件的告警"
                : ignoredMode === "ignored"
                  ? "没有本地忽略的告警"
                  : state === "dismissed"
                    ? "没有 GitHub 已忽略的告警"
                    : "暂无安全告警"
            }
            description={
              filtersActive
                ? "可尝试调整或清除筛选条件后重试。"
                : ignoredMode === "ignored"
                  ? "在本系统忽略的告警会显示在这里（不回写 GitHub）。"
                  : state === "dismissed"
                    ? "GitHub 侧已忽略的告警会显示在这里。"
                    : "开启 GitHub 仓库的安全功能后，告警会自动同步到这里。"
            }
            action={<Link to="/github">查看权限配置</Link>}
          />
        }
      >
        <>
          <ul className="event-list">
            {items.map((a) => {
              const num = a.alert_number ?? 0;
              const label = (a.rule_or_dependency || a.severity || "").trim() || "告警";
              return (
                <li key={a.id}>
                  <span className={`event-kind state-${a.state || "open"}`}>{a.alert_kind || "alert"}</span>
                  {a.ignored && <span className="ignored-badge">已忽略</span>}
                  {a.repository_full_name ? <span className="event-repo">{a.repository_full_name}</span> : null}
                  <strong>
                    #{num} {label}
                  </strong>
                  <span className="muted">
                    {a.state || "—"}
                    {a.severity ? ` · ${a.severity}` : ""}
                  </span>
                  <div className="item-actions">
                    {a.html_url ? (
                      <a className="quiet-button" href={a.html_url} target="_blank" rel="noreferrer">
                        在 GitHub 查看
                      </a>
                    ) : null}
                    <IgnoreButton
                      ignored={a.ignored || ignoredMode === "ignored"}
                      busy={busyId === a.id}
                      onToggle={() =>
                        ignoreMutation.mutate({
                          id: a.id,
                          ignored: !(a.ignored || ignoredMode === "ignored"),
                        })
                      }
                    />
                  </div>
                </li>
              );
            })}
          </ul>
          <ListFooter
            shown={items.length}
            total={total}
            hasNextPage={q.hasNextPage}
            fetchingNextPage={q.isFetchingNextPage}
            onLoadMore={() => q.fetchNextPage()}
          />
        </>
      </QueryGate>
    </ListShell>
  );
}

export function SecurityPage() {
  return (
    <FeatureGuard
      featureKey="feature.security_alerts"
      featureName="安全告警"
      description="Dependabot / Code Scanning / Secret Scanning 安全告警。"
    >
      <SecurityList />
    </FeatureGuard>
  );
}

/** 列表底部分页条：展示已加载数量与服务端总数，并提供「加载更多」翻页。 */
function ListFooter({
  shown,
  total,
  hasNextPage,
  fetchingNextPage,
  onLoadMore,
}: {
  shown: number;
  total: number;
  hasNextPage: boolean;
  fetchingNextPage: boolean;
  onLoadMore: () => void;
}) {
  return (
    <div className="list-footer">
      <span className="muted">
        已显示 {shown} / 共 {total} 条
      </span>
      {hasNextPage ? (
        <button className="quiet-button" type="button" disabled={fetchingNextPage} onClick={onLoadMore}>
          {fetchingNextPage ? "加载中…" : "加载更多"}
        </button>
      ) : null}
    </div>
  );
}


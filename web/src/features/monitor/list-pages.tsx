import { useState, type ReactNode } from "react";
import { useInfiniteQuery, useMutation, useQuery, useQueryClient } from "@tanstack/react-query";
import { Link } from "@tanstack/react-router";

import { EmptyState } from "../../components/empty-state";
import { ErrorAlert } from "../../components/error-alert";
import { QueryGate, type QueryGateQuery } from "../../components/query-gate";
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

// ---------- 分页列表共享构件 ----------

/** 分页列表查询封装：统一 per_page=50 翻页、items/total 提取，三张列表页共用。 */
function useInfiniteList<T>(opts: {
  queryKey: unknown[];
  endpoint: string;
  buildParams?: (params: URLSearchParams) => void;
}) {
  const q = useInfiniteQuery({
    queryKey: opts.queryKey,
    queryFn: ({ pageParam }) => {
      const params = new URLSearchParams({ per_page: "50", page: String(pageParam) });
      opts.buildParams?.(params);
      return apiRequest<Page<T>>(`${opts.endpoint}?${params.toString()}`);
    },
    initialPageParam: 1,
    getNextPageParam: (lastPage) =>
      lastPage.page * lastPage.per_page < lastPage.total ? lastPage.page + 1 : undefined,
  });
  const items = q.data?.pages.flatMap((page) => page.items) ?? [];
  const total = q.data?.pages[q.data.pages.length - 1]?.total ?? 0;
  return { q, items, total };
}

/** 状态/结论筛选按钮组：当前选中项高亮。 */
function StateFilterButtons({
  options,
  value,
  onChange,
}: {
  options: { value: string; label: string }[];
  value: string;
  onChange: (value: string) => void;
}) {
  return (
    <>
      {options.map((opt) => (
        <button
          key={opt.value}
          className={`quiet-button${value === opt.value ? " active" : ""}`}
          type="button"
          onClick={() => onChange(opt.value)}
        >
          {opt.label}
        </button>
      ))}
    </>
  );
}

/** 条目操作区：GitHub 查看链接 + 忽略切换。 */
function ItemActions({
  htmlUrl,
  ignored,
  busy,
  onToggleIgnore,
}: {
  htmlUrl?: string;
  ignored?: boolean;
  busy?: boolean;
  onToggleIgnore: () => void;
}) {
  return (
    <div className="item-actions">
      {htmlUrl ? (
        <a className="quiet-button" href={htmlUrl} target="_blank" rel="noreferrer">
          在 GitHub 查看
        </a>
      ) : null}
      <IgnoreButton ignored={ignored} busy={busy} onToggle={onToggleIgnore} />
    </div>
  );
}

/** 事件列表主体：QueryGate 三态守卫 + 事件列表 + 底部加载条。 */
function EventListBody({
  query,
  items,
  total,
  emptyState,
  children,
}: {
  query: QueryGateQuery & {
    hasNextPage?: boolean;
    isFetchingNextPage?: boolean;
    fetchNextPage: () => void;
  };
  items: unknown[];
  total: number;
  emptyState: ReactNode;
  children: ReactNode;
}) {
  return (
    <QueryGate query={query} isEmpty={items.length === 0} emptyState={emptyState}>
      <>
        <ul className="event-list">{children}</ul>
        <ListFooter
          shown={items.length}
          total={total}
          hasNextPage={query.hasNextPage ?? false}
          fetchingNextPage={query.isFetchingNextPage ?? false}
          onLoadMore={() => query.fetchNextPage()}
        />
      </>
    </QueryGate>
  );
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
  const { q, items, total } = useInfiniteList<WorkItem>({
    queryKey: ["work-items", kind, state, repoId, ignoredMode, reviewFilter, checkFilter],
    endpoint: "/api/v1/work-items",
    buildParams: (params) => {
      params.set("kind", kind);
      if (state) params.set("state", state);
      if (repoId) params.set("repository_id", repoId);
      if (ignoredMode === "ignored") params.set("ignored", "true");
      if (reviewFilter) params.set("review", reviewFilter);
      if (checkFilter) params.set("check", checkFilter);
    },
  });

  const { mutation: ignoreMutation, busyId } = useIgnoreMutation(setWorkItemIgnored, ["work-items"]);
  // 仓库/审核/检查筛选激活时，空态需区分「筛选后为空」与「真的没有」。
  const filtersActive = repoId !== "" || reviewFilter !== "" || checkFilter !== "";

  return (
    <ListShell eyebrow="仓库" title={title} description={description}>
      <div className="filter-bar filter-bar--wrap">
        <StateFilterButtons
          options={[
            { value: "", label: "全部" },
            { value: "open", label: "未关闭" },
            { value: "closed", label: "已关闭" },
          ]}
          value={state}
          onChange={setState}
        />
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
      <EventListBody
        query={q}
        items={items}
        total={total}
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
                  <ItemActions
                    htmlUrl={it.html_url}
                    ignored={it.ignored || ignoredMode === "ignored"}
                    busy={busyId === it.id}
                    onToggleIgnore={() =>
                      ignoreMutation.mutate({
                        id: it.id,
                        ignored: !(it.ignored || ignoredMode === "ignored"),
                      })
                    }
                  />
              </li>
            );
            })}
      </EventListBody>
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
    <ListShell eyebrow="仓库" title="仓库管理" description="「监控」为总开关；子能力受全局功能模块约束。本系统归档会停采集（可撤销），与 GitHub 侧已归档是两回事。">
      {errorMsg ? <ErrorAlert title="更新失败" message={errorMsg} /> : null}
      <div className="filter-bar">
        <button className={`quiet-button${!showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(false)}>
          未归档 ({activeRepos.length})
        </button>
        <button className={`quiet-button${showArchived ? " active" : ""}`} type="button" onClick={() => setShowArchived(true)}>
          本系统已归档 ({archivedRepos.length})
        </button>
      </div>
      <QueryGate
        query={repos}
        errorTitle="无法加载仓库列表"
        isEmpty={displayed.length === 0}
        emptyState={
          <EmptyState
            title={showArchived ? "没有本系统归档的仓库" : "暂无关注的仓库"}
            description={
              showArchived
                ? "在本系统归档的仓库会显示在这里。"
                : "安装 GitHub App 后仓库会自动出现；也可在 GitHub 页登记外部公开仓。"
            }
            action={
              showArchived ? undefined : (
                <span className="link-row" style={{ justifyContent: "center", flexWrap: "wrap" }}>
                  <Link to="/github">打开 GitHub App 页</Link>
                </span>
              )
            }
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
  const monitorOn = repo.monitor_enabled;
  function handleArchive(next: boolean) {
    if (next) {
      if (
        !window.confirm(
          `确定在本系统归档「${repo.full_name || repo.name}」？将关闭监控与全部能力开关，停止采集与通知（不修改 GitHub 侧归档状态）。`,
        )
      ) {
        return;
      }
    }
    onToggle({ is_archived: next });
  }

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
            {repo.is_archived ? " · 本系统已归档" : ""}
          </span>
          {repo.last_synced_at && <span className="last-synced">最后同步: {formatRelativeTime(repo.last_synced_at)}</span>}
          {repo.last_sync_error_code && <span className="sync-error">同步错误: {repo.last_sync_error_code}</span>}
        </div>
      </div>
      <div className="repo-card__toggles">
        <Toggle
          label="监控（总开关）"
          checked={monitorOn}
          disabled={saving}
          onChange={(v) => onToggle({ monitor_enabled: v })}
        />
        <div className="repo-card__capability-group" aria-label="能力开关">
          <CapabilityToggle
            label="Issues"
            repoEnabled={repo.issues_enabled}
            globalOn={features.issues}
            monitorOn={monitorOn}
            saving={saving}
            onChange={(v) => onToggle({ issues_enabled: v })}
          />
          <CapabilityToggle
            label="PR"
            repoEnabled={repo.pr_enabled}
            globalOn={features.prs}
            monitorOn={monitorOn}
            saving={saving}
            onChange={(v) => onToggle({ pr_enabled: v })}
          />
          <CapabilityToggle
            label="Actions"
            repoEnabled={repo.actions_enabled}
            globalOn={features.actions}
            monitorOn={monitorOn}
            saving={saving}
            onChange={(v) => onToggle({ actions_enabled: v })}
          />
          <CapabilityToggle
            label="安全告警"
            repoEnabled={repo.alerts_enabled}
            globalOn={features.alerts}
            monitorOn={monitorOn}
            saving={saving}
            onChange={(v) => onToggle({ alerts_enabled: v })}
          />
        </div>
        <Toggle label="本系统归档" checked={repo.is_archived} disabled={saving} onChange={handleArchive} />
      </div>
    </li>
  );
}

/** 能力开关：展示「有效开」与全局/仓级原因，不把仓级配置静默写成 false。 */
function CapabilityToggle({
  label,
  repoEnabled,
  globalOn,
  monitorOn,
  saving,
  onChange,
}: {
  label: string;
  repoEnabled: boolean;
  globalOn: boolean;
  monitorOn: boolean;
  saving: boolean;
  onChange: (v: boolean) => void;
}) {
  const effective = globalOn && monitorOn && repoEnabled;
  let hint = "";
  if (!globalOn) hint = "全局关闭";
  else if (!monitorOn) hint = "监控已关";
  const disabled = saving || !globalOn || !monitorOn;
  return (
    <label className={`toggle-row${!globalOn ? " toggle-row--global-off" : ""}`} title={hint || undefined}>
      <input
        type="checkbox"
        checked={effective}
        disabled={disabled}
        aria-label={hint ? `${label}（${hint}）` : label}
        onChange={(e) => onChange(e.target.checked)}
      />
      <span>
        {label}
        {hint ? <em className="toggle-hint"> · {hint}</em> : null}
      </span>
    </label>
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

function formatAlertKind(kind?: string): string {
  switch (kind) {
    case "dependabot":
      return "依赖漏洞";
    case "code_scanning":
      return "代码扫描";
    case "secret_scanning":
      return "密钥泄露";
    default:
      return kind || "告警";
  }
}

function ActionsList() {
  const { active: activeRepos } = useActiveRepos();
  const [repoId, setRepoId] = useState<string>("");
  const [conclusion, setConclusion] = useState<string>("");
  const [ignoredMode, setIgnoredMode] = useState<IgnoredMode>("active");
  const { mutation: ignoreMutation, busyId } = useIgnoreMutation(setWorkflowRunIgnored, ["workflow-runs"]);

  const { q, items, total } = useInfiniteList<WorkflowRun>({
    queryKey: ["workflow-runs", repoId, conclusion, ignoredMode],
    endpoint: "/api/v1/workflow-runs",
    buildParams: (params) => {
      if (repoId) params.set("repository_id", repoId);
      if (conclusion) params.set("conclusion", conclusion);
      if (ignoredMode === "ignored") params.set("ignored", "true");
    },
  });
  const filtersActive = repoId !== "" || conclusion !== "";

  return (
    <ListShell
      eyebrow="仓库"
      title="Actions"
      description="Workflow Run 结论与恢复状态。依赖 GitHub App 的 Actions 只读权限与 workflow_run 事件；也可在仪表盘触发对账补拉。"
    >
      <div className="filter-bar filter-bar--wrap">
        <StateFilterButtons
          options={[
            { value: "", label: "全部" },
            { value: "failure", label: "失败" },
            { value: "success", label: "成功" },
            { value: "cancelled", label: "已取消" },
          ]}
          value={conclusion}
          onChange={setConclusion}
        />
        <IgnoredToggle mode={ignoredMode} onChange={setIgnoredMode} />
        <span className="filter-bar__sep" />
        <RepoFilterSelect value={repoId} onChange={setRepoId} repos={activeRepos} />
      </div>
      <EventListBody
        query={q}
        items={items}
        total={total}
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
                  <ItemActions
                    htmlUrl={run.html_url}
                    ignored={run.ignored || ignoredMode === "ignored"}
                    busy={busyId === run.id}
                    onToggleIgnore={() =>
                      ignoreMutation.mutate({
                        id: run.id,
                        ignored: !(run.ignored || ignoredMode === "ignored"),
                      })
                    }
                  />
              </li>
            );
            })}
      </EventListBody>
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

  const { q, items, total } = useInfiniteList<SecurityAlert>({
    queryKey: ["security-alerts", state, alertKind, repoId, ignoredMode],
    endpoint: "/api/v1/security-alerts",
    buildParams: (params) => {
      if (state) params.set("state", state);
      if (alertKind) params.set("alert_kind", alertKind);
      if (repoId) params.set("repository_id", repoId);
      if (ignoredMode === "ignored") params.set("ignored", "true");
    },
  });
  const filtersActive = repoId !== "" || alertKind !== "";

  return (
    <ListShell eyebrow="仓库" title="安全告警" description="Dependabot / Code Scanning / Secret Scanning 安全告警。">
      <div className="filter-bar filter-bar--wrap">
        <StateFilterButtons
          options={[
            { value: "", label: "全部" },
            { value: "open", label: "待处理" },
            { value: "dismissed", label: "GitHub 已忽略" },
          ]}
          value={state}
          onChange={setState}
        />
        <IgnoredToggle mode={ignoredMode} onChange={setIgnoredMode} />
        <span className="filter-bar__sep" />
        <label className="repo-filter">
          <span className="sr-only">告警类型</span>
          <select value={alertKind} onChange={(e) => setAlertKind(e.target.value)} aria-label="告警类型">
            <option value="">全部类型</option>
            <option value="dependabot">依赖漏洞</option>
            <option value="code_scanning">代码扫描</option>
            <option value="secret_scanning">密钥泄露</option>
          </select>
        </label>
        <span className="filter-bar__sep" />
        <RepoFilterSelect value={repoId} onChange={setRepoId} repos={activeRepos} />
      </div>
      <EventListBody
        query={q}
        items={items}
        total={total}
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
        {items.map((a) => {
            const num = a.alert_number ?? 0;
            const label = (a.rule_or_dependency || a.severity || "").trim() || "告警";
            return (
              <li key={a.id}>
                  <span className={`event-kind state-${a.state || "open"}`}>{formatAlertKind(a.alert_kind)}</span>
                  {a.ignored && <span className="ignored-badge">已忽略</span>}
                  {a.repository_full_name ? <span className="event-repo">{a.repository_full_name}</span> : null}
                  <strong>
                    #{num} {label}
                  </strong>
                  <span className="muted">
                    {a.state || "—"}
                    {a.severity ? ` · ${a.severity}` : ""}
                  </span>
                  <ItemActions
                    htmlUrl={a.html_url}
                    ignored={a.ignored || ignoredMode === "ignored"}
                    busy={busyId === a.id}
                    onToggleIgnore={() =>
                      ignoreMutation.mutate({
                        id: a.id,
                        ignored: !(a.ignored || ignoredMode === "ignored"),
                      })
                    }
                  />
              </li>
            );
            })}
      </EventListBody>
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


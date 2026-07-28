import type { ReactNode } from "react";
import { useQuery } from "@tanstack/react-query";

import { EmptyState } from "../../components/empty-state";
import { apiRequest } from "../../lib/api/client";

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
}

interface WorkflowRun {
  id: string;
  workflow_name: string;
  run_number: number;
  head_branch: string;
  conclusion?: string | null;
  status: string;
  html_url: string;
}

interface SecurityAlert {
  id: string;
  alert_kind: string;
  alert_number: number;
  state: string;
  severity: string;
  rule_or_dependency: string;
  html_url: string;
}

interface Installation {
  id: string;
  installation_id: number;
  account_login: string;
  account_type: string;
  suspended: string;
}

export function WorkItemsPage() {
  const kind = typeof window !== "undefined" ? new URLSearchParams(window.location.search).get("kind") || "" : "";
  const q = useQuery({
    queryKey: ["work-items", kind],
    queryFn: () =>
      apiRequest<Page<WorkItem>>(`/api/v1/work-items?per_page=50${kind ? `&kind=${encodeURIComponent(kind)}` : ""}`),
  });
  return (
    <ListShell title="Issues / Pull Requests" description="自有与关注仓库的工作项。">
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((it) => (
            <li key={it.id}>
              <span className="event-kind">{it.kind}</span>
              <strong>
                #{it.number} {it.title}
              </strong>
              <span className="muted">{it.state}</span>
              {it.html_url ? (
                <a href={it.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState title="暂无工作项" description="Webhook 或对账同步后将显示 Issue 与 PR。" />
      )}
    </ListShell>
  );
}

export function ActionsPage() {
  const q = useQuery({
    queryKey: ["workflow-runs"],
    queryFn: () => apiRequest<Page<WorkflowRun>>("/api/v1/workflow-runs?per_page=50"),
  });
  return (
    <ListShell title="Actions" description="Workflow Run 结论与恢复状态。">
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((run) => (
            <li key={run.id}>
              <span className="event-kind">{run.conclusion || run.status}</span>
              <strong>
                {run.workflow_name} #{run.run_number}
              </strong>
              <span className="muted">{run.head_branch}</span>
              {run.html_url ? (
                <a href={run.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState title="暂无 Actions 运行" description="安装 App 并授予 Actions 读取权限后可见。" />
      )}
    </ListShell>
  );
}

export function SecurityPage() {
  const q = useQuery({
    queryKey: ["security-alerts"],
    queryFn: () => apiRequest<Page<SecurityAlert>>("/api/v1/security-alerts?per_page=50"),
  });
  return (
    <ListShell title="安全告警" description="Dependabot / Code Scanning / Secret Scanning。">
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((a) => (
            <li key={a.id}>
              <span className="event-kind">{a.alert_kind}</span>
              <strong>
                #{a.alert_number} {a.rule_or_dependency || a.severity}
              </strong>
              <span className="muted">
                {a.state} · {a.severity}
              </span>
              {a.html_url ? (
                <a href={a.html_url} target="_blank" rel="noreferrer">
                  打开
                </a>
              ) : null}
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState title="暂无安全告警" description="开启仓库安全功能并授权 App 后将显示告警。" />
      )}
    </ListShell>
  );
}

export function GitHubPage() {
  const q = useQuery({
    queryKey: ["installations"],
    queryFn: () => apiRequest<{ items: Installation[] }>("/api/v1/github/installations"),
  });
  return (
    <ListShell title="GitHub App" description="已记录的 Installation。Webhook 地址为 /webhooks/github。">
      {q.data?.items.length ? (
        <ul className="event-list">
          {q.data.items.map((inst) => (
            <li key={inst.id}>
              <span className="event-kind">{inst.account_type}</span>
              <strong>{inst.account_login}</strong>
              <span className="muted">installation {inst.installation_id}</span>
              <span className="muted">{inst.suspended === "true" ? "已挂起" : "正常"}</span>
            </li>
          ))}
        </ul>
      ) : (
        <EmptyState
          title="尚未收到 Installation"
          description="在 GitHub 创建 App、配置 Webhook Secret，并安装到仓库后，installation 事件会写入此处。"
        />
      )}
    </ListShell>
  );
}

export function AboutPage() {
  const q = useQuery({
    queryKey: ["version"],
    queryFn: () => apiRequest<Record<string, string>>("/api/v1/system/version"),
  });
  const v = q.data || {};
  return (
    <ListShell title="关于与版本" description="构建元数据与运行信息。">
      <ul className="event-list">
        <li>
          <strong>版本</strong> <span>{v.version || "—"}</span>
        </li>
        <li>
          <strong>Git SHA</strong> <span className="muted">{v.git_sha || "—"}</span>
        </li>
        <li>
          <strong>构建时间</strong> <span className="muted">{v.build_time || "—"}</span>
        </li>
        <li>
          <strong>渠道</strong> <span className="muted">{v.build_channel || "—"}</span>
        </li>
        <li>
          <strong>数据库</strong> <span className="muted">{v.database_driver || "—"}</span>
        </li>
        <li>
          <strong>Schema</strong> <span className="muted">{v.schema_version || "—"}</span>
        </li>
      </ul>
    </ListShell>
  );
}

function ListShell({
  title,
  description,
  children,
}: {
  title: string;
  description: string;
  children: ReactNode;
}) {
  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">仓库值守</p>
          <h1>{title}</h1>
          <p>{description}</p>
        </div>
      </section>
      <section className="onboarding-card">{children}</section>
    </>
  );
}

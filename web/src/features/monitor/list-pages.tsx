import type { ReactNode } from "react";
import { useState } from "react";
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

type VersionInfo = {
  version?: string;
  git_sha?: string;
  git_branch?: string;
  build_time?: string;
  build_channel?: string;
  go_version?: string;
  database_driver?: string;
  schema_version?: string;
  update_check_enabled?: boolean;
};

type UpdateCheckInfo = {
  enabled: boolean;
  latest_version?: string;
  latest_url?: string;
  update_available: boolean;
  checked_at?: string;
  error?: string;
  source?: string;
  cached?: boolean;
};

type VersionCheckResponse = {
  version: VersionInfo;
  update_check: UpdateCheckInfo;
};

function safeHttpUrl(raw?: string | null): string | null {
  if (!raw) return null;
  try {
    const u = new URL(String(raw).trim());
    if (u.protocol !== "https:" && u.protocol !== "http:") return null;
    return u.toString();
  } catch {
    return null;
  }
}

export function AboutPage() {
  const q = useQuery({
    queryKey: ["version"],
    queryFn: () => apiRequest<VersionInfo>("/api/v1/system/version"),
  });
  const [checking, setChecking] = useState(false);
  const [banner, setBanner] = useState<{
    kind: "update" | "latest" | "error" | "info";
    text: string;
    url?: string | null;
  } | null>(null);

  const v = q.data || {};
  const checkUpdate = async (force = true) => {
    setChecking(true);
    try {
      const res = await apiRequest<VersionCheckResponse>(
        `/api/v1/system/version/check?force=${force ? "true" : "false"}`,
        { method: "POST", body: JSON.stringify({}) },
      );
      const uc = res.update_check;
      if (!uc.enabled) {
        setBanner({ kind: "info", text: "远程更新检查已关闭（REPOSENTINEL_UPDATE_CHECK）。" });
        return;
      }
      if (uc.error && !uc.latest_version) {
        setBanner({ kind: "error", text: `检查失败：${uc.error}` });
        return;
      }
      const url = safeHttpUrl(uc.latest_url);
      if (uc.update_available && uc.latest_version) {
        setBanner({
          kind: "update",
          text: `发现新版本 v${uc.latest_version}（当前 v${res.version.version || v.version || "—"}）`,
          url,
        });
        return;
      }
      setBanner({
        kind: "latest",
        text: uc.latest_version
          ? `已是最新（远程 v${uc.latest_version}${uc.cached ? " · 缓存" : ""}）`
          : "未获取到远程版本信息",
        url,
      });
    } catch (e) {
      setBanner({
        kind: "error",
        text: e instanceof Error ? e.message : "检查更新失败",
      });
    } finally {
      setChecking(false);
    }
  };

  return (
    <ListShell title="关于与版本" description="构建元数据、运行信息与 GitHub Release 更新检查。">
      <div className="about-toolbar">
        <button
          type="button"
          className="quiet-button"
          disabled={checking || q.isLoading}
          onClick={() => void checkUpdate(true)}
        >
          {checking ? "检查中…" : "检查更新"}
        </button>
      </div>
      {banner ? (
        <div
          className={
            banner.kind === "update"
              ? "about-banner about-banner--update"
              : banner.kind === "latest"
                ? "about-banner about-banner--latest"
                : banner.kind === "error"
                  ? "about-banner about-banner--error"
                  : "about-banner about-banner--info"
          }
        >
          <div>{banner.text}</div>
          {banner.kind === "update" ? (
            <p className="muted">升级请拉取新镜像或替换二进制，并阅读 CHANGELOG。</p>
          ) : null}
          {banner.url ? (
            <a href={banner.url} target="_blank" rel="noopener noreferrer">
              打开 Release 页面
            </a>
          ) : null}
        </div>
      ) : null}
      <ul className="event-list">
        <li>
          <strong>版本</strong> <span>{v.version || "—"}</span>
        </li>
        <li>
          <strong>Git SHA</strong> <span className="muted">{v.git_sha || "—"}</span>
        </li>
        <li>
          <strong>分支</strong> <span className="muted">{v.git_branch || "—"}</span>
        </li>
        <li>
          <strong>构建时间</strong> <span className="muted">{v.build_time || "—"}</span>
        </li>
        <li>
          <strong>渠道</strong> <span className="muted">{v.build_channel || "—"}</span>
        </li>
        <li>
          <strong>Go</strong> <span className="muted">{v.go_version || "—"}</span>
        </li>
        <li>
          <strong>数据库</strong> <span className="muted">{v.database_driver || "—"}</span>
        </li>
        <li>
          <strong>Schema</strong> <span className="muted">{v.schema_version || "—"}</span>
        </li>
        <li>
          <strong>更新检查</strong>{" "}
          <span className="muted">{v.update_check_enabled === false ? "已关闭" : "已开启"}</span>
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

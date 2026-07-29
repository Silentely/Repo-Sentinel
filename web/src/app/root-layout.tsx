import {
  Activity,
  Archive,
  Bell,
  GitPullRequest,
  FolderGit2,
  Info,
  ListTodo,
  LogOut,
  Send,
  Shield,
  ShieldCheck,
  Workflow,
} from "lucide-react";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";

import { ThemeToggle } from "../components/theme-toggle";
import {
  logout,
  readyStatusQueryOptions,
  type AuthenticationResponse,
} from "../features/auth/api";
import { dashboardQueryOptions } from "../features/monitor/api";

export interface RootLayoutProps {
  session: AuthenticationResponse;
}

export function RootLayout({ session }: RootLayoutProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [loggingOut, setLoggingOut] = useState(false);
  const ready = useQuery(readyStatusQueryOptions);
  const dashboard = useQuery(dashboardQueryOptions);

  const healthState = ready.isPending
    ? "warning"
    : ready.isError || ready.data?.status !== "ready"
      ? "error"
      : "ready";
  const healthLabel = healthState === "ready" ? "服务正常" : healthState === "warning" ? "检查中" : "需要关注";

  // 侧边栏徽章数据
  const stats = dashboard.data;
  const openIssues = stats?.open_issues ?? 0;
  const openPRs = stats?.open_pulls ?? 0;
  const failedActions = stats?.failed_actions ?? 0;
  const openSecurity = stats?.open_security ?? 0;
  const outboxDead = stats?.outbox_dead ?? 0;

  async function handleLogout() {
    setLoggingOut(true);
    try {
      await logout();
    } catch {
      // Session 无论服务端是否已撤销，都在本地清空，避免把旧凭据留在缓存中。
    } finally {
      queryClient.clear();
      setLoggingOut(false);
      await navigate({ to: "/login", replace: true });
    }
  }

  return (
    <div className="app-shell">
      <aside className="app-sidebar">
        <div className="app-brand">
          <span className="app-brand__mark" aria-hidden="true">
            <ShieldCheck size={18} strokeWidth={1.8} />
          </span>
          <span>RepoSentinel</span>
        </div>
        <nav className="app-nav" aria-label="主导航">
          <span className="app-nav__label">概览</span>
          <Link to="/" activeProps={{ "aria-current": "page" }}>
            <Activity aria-hidden="true" size={17} />
            <span>仪表盘</span>
          </Link>
          <span className="app-nav__label">仓库</span>
          <Link to="/repos" activeProps={{ "aria-current": "page" }}>
            <Archive aria-hidden="true" size={17} />
            <span>仓库管理</span>
          </Link>
          <Link to="/issues" activeProps={{ "aria-current": "page" }}>
            <ListTodo aria-hidden="true" size={17} />
            <span>Issues</span>
            {openIssues > 0 && <span className="nav-badge">{openIssues}</span>}
          </Link>
          <Link to="/pull-requests" activeProps={{ "aria-current": "page" }}>
            <GitPullRequest aria-hidden="true" size={17} />
            <span>Pull Requests</span>
            {openPRs > 0 && <span className="nav-badge">{openPRs}</span>}
          </Link>
          <Link to="/actions" activeProps={{ "aria-current": "page" }}>
            <Workflow aria-hidden="true" size={17} />
            <span>Actions</span>
            {failedActions > 0 && <span className="nav-badge nav-badge--warning">{failedActions}</span>}
          </Link>
          <Link to="/security" activeProps={{ "aria-current": "page" }}>
            <Shield aria-hidden="true" size={17} />
            <span>安全告警</span>
            {openSecurity > 0 && <span className="nav-badge nav-badge--danger">{openSecurity}</span>}
          </Link>
          <span className="app-nav__label">通知</span>
          <Link to="/notifications" activeProps={{ "aria-current": "page" }} activeOptions={{ exact: true }}>
            <Bell aria-hidden="true" size={17} />
            <span>渠道配置</span>
            {outboxDead > 0 && <span className="nav-badge nav-badge--warning">{outboxDead}</span>}
          </Link>
          <Link to="/notifications/outbox" activeProps={{ "aria-current": "page" }}>
            <Send aria-hidden="true" size={17} />
            <span>投递记录</span>
          </Link>
          <span className="app-nav__label">系统</span>
          <Link to="/github" activeProps={{ "aria-current": "page" }}>
            <FolderGit2 aria-hidden="true" size={17} />
            <span>GitHub App</span>
          </Link>
          <Link to="/about" activeProps={{ "aria-current": "page" }}>
            <Info aria-hidden="true" size={17} />
            <span>关于与设置</span>
          </Link>
        </nav>
        <span className="app-sidebar__version">RepoSentinel</span>
      </aside>

      <div className="app-main">
        <header className="app-topbar">
          <p className="app-topbar__title">管理控制台</p>
          <div className="app-topbar__actions">
            <span className={`health-pill health-pill--${healthState}`}>{healthLabel}</span>
            <ThemeToggle />
            <span className="sr-only" id="current-admin">
              当前管理员 {session.admin.username}
            </span>
            <button className="quiet-button" type="button" onClick={handleLogout} disabled={loggingOut}>
              <LogOut aria-hidden="true" size={16} />
              <span>{loggingOut ? "退出中…" : "退出"}</span>
            </button>
          </div>
        </header>
        <main className="app-content">
          <Outlet />
        </main>
      </div>
    </div>
  );
}

export function RouteLoading() {
  return (
    <main className="auth-shell" aria-busy="true">
      <section className="auth-card">
        <p className="eyebrow">RepoSentinel</p>
        <h1>正在确认 Session…</h1>
      </section>
    </main>
  );
}

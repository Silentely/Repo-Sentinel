import {
  Activity,
  GitBranch,
  LogOut,
  ShieldCheck,
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

export interface RootLayoutProps {
  session: AuthenticationResponse;
}

export function RootLayout({ session }: RootLayoutProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [loggingOut, setLoggingOut] = useState(false);
  const ready = useQuery(readyStatusQueryOptions);

  const healthState = ready.isPending
    ? "warning"
    : ready.isError || ready.data?.status !== "ready"
      ? "error"
      : "ready";
  const healthLabel = healthState === "ready" ? "服务正常" : healthState === "warning" ? "检查中" : "需要关注";

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
          <span className="app-nav__label">通知</span>
          <Link to="/notifications" activeProps={{ "aria-current": "page" }}>
            <GitBranch aria-hidden="true" size={17} />
            <span>渠道配置</span>
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

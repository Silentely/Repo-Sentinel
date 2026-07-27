import {
  Activity,
  CheckCircle2,
  CircleDashed,
  GitBranch,
  LogOut,
  ShieldCheck,
} from "lucide-react";
import { useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate } from "@tanstack/react-router";

import { ThemeToggle } from "../components/theme-toggle";
import { EmptyState } from "../components/empty-state";
import {
  logout,
  readyStatusQueryOptions,
  setupStatusQueryOptions,
  type AuthenticationResponse,
} from "../features/auth/api";
import { toApiError } from "../lib/api/errors";

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
  const healthLabel = healthState === "ready" ? "同步正常" : healthState === "warning" ? "检查中" : "需要关注";

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
          <span className="app-nav__label">系统</span>
          <span>
            <GitBranch aria-hidden="true" size={17} />
            <span>GitHub App · 后续</span>
          </span>
        </nav>
        <span className="app-sidebar__version">vdev · Phase 1</span>
      </aside>

      <div className="app-main">
        <header className="app-topbar">
          <p className="app-topbar__title">仪表盘</p>
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

export function FoundationHome() {
  const ready = useQuery(readyStatusQueryOptions);
  const setup = useQuery(setupStatusQueryOptions);
  const setupComplete = setup.data?.required === false;

  return (
    <>
      <section className="page-intro">
        <div>
          <p className="eyebrow">值守概览 · Phase 1</p>
          <h1>先确认系统，再接入仓库。</h1>
          <p>这里展示当前实例真实的健康与上手状态。GitHub 事件采集将在下一阶段接入。</p>
        </div>
      </section>

      <section className="status-card" aria-labelledby="status-title">
        <div className="status-grid">
          <div className="status-item">
            <span className="status-item__label">HTTP readiness</span>
            <strong>{ready.isPending ? "检查中" : ready.data?.status === "ready" ? "已就绪" : "待处理"}</strong>
          </div>
          <div className="status-item">
            <span className="status-item__label">管理员 Session</span>
            <strong>已保护</strong>
          </div>
          <div className="status-item">
            <span className="status-item__label">数据存储</span>
            <strong>由服务端管理</strong>
          </div>
        </div>
        <h2 id="status-title" className="sr-only">系统状态</h2>
        {ready.isError ? (
          <EmptyState
            eyebrow={toApiError(ready.error).errorCode}
            title="服务健康状态暂不可读"
            description="HTTP 服务没有返回 readiness。检查服务日志后重试。"
            action={<button className="quiet-button" type="button" onClick={() => void ready.refetch()}>重试</button>}
          />
        ) : null}
      </section>

      <section className="onboarding-card" aria-labelledby="onboarding-title">
        <h2 id="onboarding-title">上手进度</h2>
        <p>完成这些基础步骤后，下一阶段即可开始接收 GitHub 仓库事件。</p>
        <ol className="onboarding-list">
          <li data-state={setupComplete ? "done" : "next"}>
            {setupComplete ? <CheckCircle2 aria-hidden="true" size={18} /> : <CircleDashed aria-hidden="true" size={18} />}
            <span>{setupComplete ? "唯一管理员已创建" : "完成管理员初始化"}</span>
          </li>
          <li data-state="next">
            <CircleDashed aria-hidden="true" size={18} />
            <span>配置 GitHub App 与 Webhook（下一阶段）</span>
          </li>
          <li data-state="next">
            <CircleDashed aria-hidden="true" size={18} />
            <span>等待首个仓库完成基线同步</span>
          </li>
        </ol>
      </section>
    </>
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

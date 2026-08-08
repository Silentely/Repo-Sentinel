import {
  Activity,
  Archive,
  Bell,
  GitPullRequest,
  FolderGit2,
  Info,
  ListTodo,
  LogOut,
  Menu,
  Send,
  Settings,
  Shield,
  ShieldCheck,
  Workflow,
  X,
} from "lucide-react";
import { useEffect, useRef, useState } from "react";
import { useQuery, useQueryClient } from "@tanstack/react-query";
import { Link, Outlet, useNavigate, useRouterState } from "@tanstack/react-router";

import { ThemeToggle } from "../components/theme-toggle";
import {
  logout,
  readyStatusQueryOptions,
  type AuthenticationResponse,
} from "../features/auth/api";
import { dashboardQueryOptions, settingsQueryOptions, versionQueryOptions } from "../features/monitor/api";

export interface RootLayoutProps {
  session: AuthenticationResponse;
}

/** 移动端顶栏标题：侧边栏收起后需要向用户明示当前位置。
 * 与路由树（router.tsx）一一对应，导出供单测锁定，防止路由调整后标题失配。 */
export function mobileTitleFor(pathname: string): string {
  if (pathname.startsWith("/notifications/outbox")) return "投递记录";
  if (pathname.startsWith("/notifications")) return "渠道配置";
  if (pathname.startsWith("/repos")) return "仓库管理";
  if (pathname.startsWith("/issues")) return "Issues";
  if (pathname.startsWith("/pull-requests")) return "Pull Requests";
  if (pathname.startsWith("/actions")) return "Actions";
  if (pathname.startsWith("/security")) return "安全告警";
  if (pathname.startsWith("/github")) return "GitHub App";
  if (pathname.startsWith("/about")) return "关于";
  if (pathname.startsWith("/settings")) return "设置";
  return "仪表盘";
}

/** 浏览器标签页标题：复用移动端标题映射加品牌后缀，避免两套页面名文案漂移。 */
export function pageTitleFor(pathname: string): string {
  return `${mobileTitleFor(pathname)} · RepoSentinel`;
}

export function RootLayout({ session }: RootLayoutProps) {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const [loggingOut, setLoggingOut] = useState(false);
  const ready = useQuery(readyStatusQueryOptions);
  const dashboard = useQuery(dashboardQueryOptions);
  const settings = useQuery(settingsQueryOptions);
  const version = useQuery(versionQueryOptions);

  // 移动端抽屉导航：≤640px 时侧边栏变为离屏抽屉，由顶栏菜单按钮唤起。
  const [navOpen, setNavOpen] = useState(false);
  const sidebarRef = useRef<HTMLElement>(null);
  const menuButtonRef = useRef<HTMLButtonElement>(null);
  const pathname = useRouterState({ select: (state) => state.location.pathname });

  // 浏览器标签页标题随路由更新：多标签场景下能直接看出当前页面。
  useEffect(() => {
    document.title = pageTitleFor(pathname);
  }, [pathname]);

  // 离开当前路由（含浏览器前进/后退）后收起抽屉，避免遮挡新页面。
  const previousPathname = useRef(pathname);
  useEffect(() => {
    if (previousPathname.current !== pathname) {
      previousPathname.current = pathname;
      setNavOpen(false);
    }
  }, [pathname]);

  // 视口变宽超出移动端断点（如横屏、窗口拉伸）时强制收起，解除滚动锁。
  useEffect(() => {
    const media = window.matchMedia("(max-width: 640px)");
    const onChange = (event: MediaQueryListEvent) => {
      if (!event.matches) {
        setNavOpen(false);
      }
    };
    media.addEventListener("change", onChange);
    return () => media.removeEventListener("change", onChange);
  }, []);

  // 抽屉打开期间：锁定背景滚动、支持 Escape 关闭。
  useEffect(() => {
    if (!navOpen) {
      return;
    }
    const previousOverflow = document.body.style.overflow;
    document.body.style.overflow = "hidden";
    const onKeyDown = (event: KeyboardEvent) => {
      if (event.key === "Escape") {
        setNavOpen(false);
      }
    };
    document.addEventListener("keydown", onKeyDown);
    return () => {
      document.body.style.overflow = previousOverflow;
      document.removeEventListener("keydown", onKeyDown);
    };
  }, [navOpen]);

  // 焦点管理：打开时进入抽屉，关闭（含任意关闭路径）后回到菜单按钮。
  useEffect(() => {
    if (!navOpen) {
      return;
    }
    const menuButton = menuButtonRef.current;
    sidebarRef.current?.querySelector<HTMLElement>(".app-sidebar__close")?.focus();
    return () => menuButton?.focus();
  }, [navOpen]);

  function closeNav() {
    setNavOpen(false);
  }

  /** 点击导航链接即时收起抽屉；事件委托处理，避免给每个链接重复绑定。 */
  function handleNavClick(event: React.MouseEvent<HTMLElement>) {
    if ((event.target as HTMLElement).closest("a")) {
      setNavOpen(false);
    }
  }

  const healthState = ready.isPending
    ? "warning"
    : ready.isError || ready.data?.status !== "ready"
      ? "error"
      : "ready";
  const healthLabel = healthState === "ready" ? "服务正常" : healthState === "warning" ? "检查中" : "需要关注";

  // 功能模块开关：关闭后侧栏隐藏对应入口（与 FeatureGuard / 采集门禁一致）。
  const featureIssues = settings.data?.["feature.issues"] !== false;
  const featurePRs = settings.data?.["feature.pull_requests"] !== false;
  const featureActions = settings.data?.["feature.actions"] !== false;
  const featureAlerts = settings.data?.["feature.security_alerts"] !== false;

  // 侧边栏徽章数据
  const stats = dashboard.data;
  const openIssues = featureIssues ? (stats?.open_issues ?? 0) : 0;
  const openPRs = featurePRs ? (stats?.open_pulls ?? 0) : 0;
  const failedActions = featureActions ? (stats?.failed_actions ?? 0) : 0;
  const openSecurity = featureAlerts ? (stats?.open_security ?? 0) : 0;
  const outboxDead = stats?.outbox_dead ?? 0;
  // 侧栏版本号：自托管应用惯例展示真实版本；查询未就绪时回退产品名。
  const versionLabel = version.data?.version ? `v${version.data.version}` : "RepoSentinel";

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
      <aside
        id="app-sidebar"
        ref={sidebarRef}
        className={`app-sidebar${navOpen ? " is-open" : ""}`}
      >
        <div className="app-brand">
          <span className="app-brand__mark" aria-hidden="true">
            <ShieldCheck size={18} strokeWidth={1.8} />
          </span>
          <span>RepoSentinel</span>
        </div>
        <button
          type="button"
          className="app-sidebar__close"
          aria-label="收起导航菜单"
          onClick={closeNav}
        >
          <X aria-hidden="true" size={18} strokeWidth={1.8} />
        </button>
        <nav className="app-nav" aria-label="主导航" onClick={handleNavClick}>
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
          {featureIssues ? (
            <Link to="/issues" activeProps={{ "aria-current": "page" }}>
              <ListTodo aria-hidden="true" size={17} />
              <span>Issues</span>
              {openIssues > 0 && <span className="nav-badge nav-badge--info">{openIssues}</span>}
            </Link>
          ) : null}
          {featurePRs ? (
            <Link to="/pull-requests" activeProps={{ "aria-current": "page" }}>
              <GitPullRequest aria-hidden="true" size={17} />
              <span>Pull Requests</span>
              {openPRs > 0 && <span className="nav-badge nav-badge--success">{openPRs}</span>}
            </Link>
          ) : null}
          {featureActions ? (
            <Link to="/actions" activeProps={{ "aria-current": "page" }}>
              <Workflow aria-hidden="true" size={17} />
              <span>Actions</span>
              {failedActions > 0 && <span className="nav-badge nav-badge--warning">{failedActions}</span>}
            </Link>
          ) : null}
          {featureAlerts ? (
            <Link to="/security" activeProps={{ "aria-current": "page" }}>
              <Shield aria-hidden="true" size={17} />
              <span>安全告警</span>
              {openSecurity > 0 && <span className="nav-badge nav-badge--danger">{openSecurity}</span>}
            </Link>
          ) : null}
          <span className="app-nav__label">通知</span>
          <Link to="/notifications" activeProps={{ "aria-current": "page" }} activeOptions={{ exact: true }}>
            <Bell aria-hidden="true" size={17} />
            <span>渠道配置</span>
          </Link>
          <Link to="/notifications/outbox" activeProps={{ "aria-current": "page" }}>
            <Send aria-hidden="true" size={17} />
            <span>投递记录</span>
            {outboxDead > 0 && <span className="nav-badge nav-badge--warning">{outboxDead}</span>}
          </Link>
          <span className="app-nav__label">系统</span>
          <Link to="/github" activeProps={{ "aria-current": "page" }}>
            <FolderGit2 aria-hidden="true" size={17} />
            <span>GitHub App</span>
          </Link>
          <Link to="/settings" activeProps={{ "aria-current": "page" }}>
            <Settings aria-hidden="true" size={17} />
            <span>设置</span>
          </Link>
          <Link to="/about" activeProps={{ "aria-current": "page" }}>
            <Info aria-hidden="true" size={17} />
            <span>关于</span>
          </Link>
        </nav>
        <span className="app-sidebar__version">{versionLabel}</span>
      </aside>

      {navOpen && <div className="app-scrim" aria-hidden="true" onClick={closeNav} />}

      <div className="app-main">
        <header className="app-topbar">
          <div className="app-topbar__lead">
            <button
              ref={menuButtonRef}
              type="button"
              className="app-topbar__menu"
              aria-expanded={navOpen}
              aria-controls="app-sidebar"
              aria-label="打开导航菜单"
              onClick={() => setNavOpen(true)}
            >
              <Menu aria-hidden="true" size={20} strokeWidth={1.8} />
            </button>
            <p className="app-topbar__title">
              <span className="app-topbar__title--desktop">管理控制台</span>
              <span className="app-topbar__title--mobile">{mobileTitleFor(pathname)}</span>
            </p>
          </div>
          <div className="app-topbar__actions">
            <span className={`health-pill health-pill--${healthState}`} role="status" aria-label={`服务状态：${healthLabel}`}>
              <span className="health-pill__label">{healthLabel}</span>
            </span>
            <ThemeToggle />
            <span className="topbar-admin" title={`管理员 ${session.admin.username}`}>
              {session.admin.username}
            </span>
            <button
              className="quiet-button"
              type="button"
              aria-label={loggingOut ? "正在退出" : "退出登录"}
              onClick={handleLogout}
              disabled={loggingOut}
            >
              <LogOut aria-hidden="true" size={16} />
              <span className="quiet-button__label">{loggingOut ? "退出中…" : "退出"}</span>
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

import { lazy, Suspense, useEffect } from "react";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
  lazyRouteComponent,
  Navigate,
  Outlet,
  useNavigate,
} from "@tanstack/react-router";
import type { QueryClient } from "@tanstack/react-query";
import { useQuery, useQueryClient } from "@tanstack/react-query";

import { createAdmin, login, setupStatusQueryOptions } from "../features/auth/api";
import { useSession } from "../features/auth/use-session";
import { EmptyState } from "../components/empty-state";
import { ApiError } from "../lib/api/errors";
import { queryClient } from "../lib/query-client";
import { RootLayout, RouteLoading } from "./root-layout";
import { RouteErrorFallback, RouteNotFoundFallback, RoutePendingFallback } from "./route-fallbacks";

// 全部页面按路由拆分 chunk（含认证链路，其表单校验库体量不小）。
const LoginPage = lazy(() => import("../features/auth/login-page").then((m) => ({ default: m.LoginPage })));
const SetupPage = lazy(() => import("../features/auth/setup-page").then((m) => ({ default: m.SetupPage })));
const DashboardPage = lazyRouteComponent(() => import("../features/monitor/dashboard-page"), "DashboardPage");
const NotifyPage = lazyRouteComponent(() => import("../features/monitor/notify-page"), "NotifyPage");
const OutboxPage = lazyRouteComponent(() => import("../features/monitor/outbox-page"), "OutboxPage");
const IssuesPage = lazyRouteComponent(() => import("../features/monitor/list-pages"), "IssuesPage");
const PullRequestsPage = lazyRouteComponent(() => import("../features/monitor/list-pages"), "PullRequestsPage");
const ReposPage = lazyRouteComponent(() => import("../features/monitor/list-pages"), "ReposPage");
const ActionsPage = lazyRouteComponent(() => import("../features/monitor/list-pages"), "ActionsPage");
const SecurityPage = lazyRouteComponent(() => import("../features/monitor/list-pages"), "SecurityPage");
const GitHubPage = lazyRouteComponent(() => import("../features/monitor/github-page"), "GitHubPage");
const AboutPage = lazyRouteComponent(() => import("../features/monitor/about-page"), "AboutPage");
const SettingsPage = lazyRouteComponent(() => import("../features/monitor/settings-page"), "SettingsPage");
const StarredReleasesPage = lazyRouteComponent(() => import("../features/monitor/starred-releases-page"), "StarredReleasesPage");

export interface RouterContext {
  queryClient: QueryClient;
}

const rootRoute = createRootRouteWithContext<RouterContext>()({
  component: () => <Outlet />,
});

const loginRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/login",
  component: LoginRoute,
});

const setupRoute = createRoute({
  getParentRoute: () => rootRoute,
  path: "/setup",
  component: SetupRoute,
});

const authenticatedRoute = createRoute({
  getParentRoute: () => rootRoute,
  id: "_authenticated",
  component: AuthenticatedRoute,
});

const indexRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/",
  component: DashboardPage,
});

const notifyRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/notifications",
  component: NotifyPage,
});

const outboxRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/notifications/outbox",
  component: OutboxPage,
});

const issuesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/issues",
  component: IssuesPage,
});

const pullRequestsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/pull-requests",
  component: PullRequestsPage,
});

const reposRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/repos",
  component: ReposPage,
});

const actionsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/actions",
  component: ActionsPage,
});

const securityRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/security",
  component: SecurityPage,
});

const githubRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/github",
  component: GitHubPage,
});

const aboutRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/about",
  component: AboutPage,
});

const settingsRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/settings",
  component: SettingsPage,
});

const starredReleasesRoute = createRoute({
  getParentRoute: () => authenticatedRoute,
  path: "/starred-releases",
  component: StarredReleasesPage,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  authenticatedRoute.addChildren([
    indexRoute,
    notifyRoute,
    outboxRoute,
    issuesRoute,
    pullRequestsRoute,
    reposRoute,
    actionsRoute,
    securityRoute,
    githubRoute,
    aboutRoute,
    settingsRoute,
    starredReleasesRoute,
  ]),
]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
  defaultErrorComponent: RouteErrorFallback,
  defaultNotFoundComponent: RouteNotFoundFallback,
  defaultPendingComponent: RoutePendingFallback,
});

declare module "@tanstack/react-router" {
  interface Register {
    router: typeof router;
  }
}

function LoginRoute() {
  const navigate = useNavigate();
  const setup = useQuery(setupStatusQueryOptions);

  useEffect(() => {
    if (setup.data?.required) {
      void navigate({ to: "/setup", replace: true });
    }
  }, [navigate, setup.data?.required]);

  return (
    <Suspense fallback={<RouteLoading />}>
      <LoginPage loginAction={login} onAuthenticated={() => void navigate({ to: "/", replace: true })} />
    </Suspense>
  );
}

function SetupRoute() {
  const navigate = useNavigate();
  const queryClient = useQueryClient();
  const setup = useQuery(setupStatusQueryOptions);

  useEffect(() => {
    if (setup.data && !setup.data.required) {
      void navigate({ to: "/login", replace: true });
    }
  }, [navigate, setup.data]);

  return (
    <Suspense fallback={<RouteLoading />}>
      <SetupPage
        setupAction={createAdmin}
        onCreated={() => {
          // 失效 setup-status 缓存：15s staleTime 内浏览器后退会命中旧缓存
          // 「required=true」被重定向回 /setup，再次提交得到后端 not_found。
          void queryClient.invalidateQueries({ queryKey: ["auth", "setup-status"] });
          void navigate({ to: "/", replace: true });
        }}
      />
    </Suspense>
  );
}

function AuthenticatedRoute() {
  const session = useSession();
  if (session.isPending) {
    return <RouteLoading />;
  }
  if (session.isError) {
    // 仅 401 视为登出；网络抖动/实例重启应允许原地重试，避免误踢登录。
    if (session.error instanceof ApiError && session.error.status === 401) {
      return <Navigate to="/login" replace />;
    }
    return (
      <EmptyState
        title="无法连接服务"
        description="网络异常或实例正在重启，登录状态仍然保留，请稍后重试。"
        actionArrow={false}
        action={
          <button type="button" className="primary-button primary-button--inline" onClick={() => void session.refetch()}>
            重新连接
          </button>
        }
      />
    );
  }
  if (!session.data) {
    return <Navigate to="/login" replace />;
  }
  return <RootLayout session={session.data} />;
}

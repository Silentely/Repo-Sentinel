import { useEffect } from "react";
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
import { useQuery } from "@tanstack/react-query";

import { LoginPage } from "../features/auth/login-page";
import { SetupPage } from "../features/auth/setup-page";
import { createAdmin, login, setupStatusQueryOptions } from "../features/auth/api";
import { useSession } from "../features/auth/use-session";
import { queryClient } from "../lib/query-client";
import { RootLayout, RouteLoading } from "./root-layout";

// 监控/功能页面按路由拆分 chunk；登录与初始化页保持静态，保证首屏认证链路即时可用。
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
  ]),
]);

export const router = createRouter({
  routeTree,
  context: { queryClient },
  defaultPreload: "intent",
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

  return <LoginPage loginAction={login} onAuthenticated={() => void navigate({ to: "/", replace: true })} />;
}

function SetupRoute() {
  const navigate = useNavigate();
  const setup = useQuery(setupStatusQueryOptions);

  useEffect(() => {
    if (setup.data && !setup.data.required) {
      void navigate({ to: "/login", replace: true });
    }
  }, [navigate, setup.data]);

  return <SetupPage setupAction={createAdmin} onCreated={() => void navigate({ to: "/", replace: true })} />;
}

function AuthenticatedRoute() {
  const session = useSession();
  if (session.isPending) {
    return <RouteLoading />;
  }
  if (session.isError || !session.data) {
    return <Navigate to="/login" replace />;
  }
  return <RootLayout session={session.data} />;
}

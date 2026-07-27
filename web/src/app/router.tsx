import { useEffect } from "react";
import {
  createRootRouteWithContext,
  createRoute,
  createRouter,
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
import { RootLayout, FoundationHome, RouteLoading } from "./root-layout";

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
  component: FoundationHome,
});

const routeTree = rootRoute.addChildren([
  loginRoute,
  setupRoute,
  authenticatedRoute.addChildren([indexRoute]),
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

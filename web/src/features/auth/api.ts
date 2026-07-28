import { queryOptions } from "@tanstack/react-query";

import { apiRequest } from "../../lib/api/client";
import type { LoginCredentials } from "./schemas";

export interface AdminIdentity {
  id: string;
  username: string;
}

export interface SessionIdentity {
  id: string;
  expires_at: string;
}

export interface AuthenticationResponse {
  admin: AdminIdentity;
  session: SessionIdentity;
}

export interface SetupStatus {
  required: boolean;
}

export interface ReadyStatus {
  status: "ready" | "not_ready";
}

export const sessionQueryOptions = queryOptions({
  queryKey: ["auth", "session"] as const,
  queryFn: () => apiRequest<AuthenticationResponse>("/api/v1/auth/session"),
  retry: false,
  staleTime: 30_000,
});

export const setupStatusQueryOptions = queryOptions({
  queryKey: ["auth", "setup-status"] as const,
  queryFn: () => apiRequest<SetupStatus>("/api/v1/setup/status"),
  staleTime: 15_000,
});

export const readyStatusQueryOptions = queryOptions({
  queryKey: ["health", "ready"] as const,
  queryFn: () => apiRequest<ReadyStatus>("/health/ready"),
  staleTime: 15_000,
  refetchInterval: 30_000,
});

export async function login(credentials: LoginCredentials): Promise<AuthenticationResponse> {
  return apiRequest<AuthenticationResponse>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify(credentials),
  });
}

export async function createAdmin(input: LoginCredentials): Promise<AuthenticationResponse> {
  return apiRequest<AuthenticationResponse>("/api/v1/setup", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function logout(): Promise<void> {
  await apiRequest<{ logged_out: boolean }>("/api/v1/auth/logout", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function changePassword(input: {
  current_password: string;
  new_password: string;
}): Promise<void> {
  await apiRequest<{ changed: boolean }>("/api/v1/auth/password", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

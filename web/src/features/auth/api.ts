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

export type LoginResult =
  | { requires_2fa: true; ticket: string }
  | AuthenticationResponse;

export async function login(credentials: LoginCredentials): Promise<LoginResult> {
  return apiRequest<LoginResult>("/api/v1/auth/login", {
    method: "POST",
    body: JSON.stringify(credentials),
  });
}

export async function login2FA(input: {
  ticket: string;
  passcode: string;
}): Promise<AuthenticationResponse> {
  return apiRequest<AuthenticationResponse>("/api/v1/auth/login/2fa", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export interface TwoFactorStatus {
  enabled: boolean;
}

export interface TwoFactorSetup {
  secret: string;
  otpauth_url: string;
}

export async function get2FAStatus(): Promise<TwoFactorStatus> {
  return apiRequest<TwoFactorStatus>("/api/v1/admin/2fa");
}

export async function setup2FA(): Promise<TwoFactorSetup> {
  return apiRequest<TwoFactorSetup>("/api/v1/admin/2fa/setup", {
    method: "POST",
    body: JSON.stringify({}),
  });
}

export async function enable2FA(input: {
  secret: string;
  passcode: string;
}): Promise<TwoFactorStatus> {
  return apiRequest<TwoFactorStatus>("/api/v1/admin/2fa/enable", {
    method: "POST",
    body: JSON.stringify(input),
  });
}

export async function disable2FA(input: {
  current_password: string;
}): Promise<TwoFactorStatus> {
  return apiRequest<TwoFactorStatus>("/api/v1/admin/2fa/disable", {
    method: "POST",
    body: JSON.stringify(input),
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

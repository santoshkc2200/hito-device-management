import {
  beginTotpReenrolment,
  changeOwnPassword,
  client,
  confirmTotpReenrolment,
  getCurrentAdmin,
  login,
  logout,
  regenerateRecoveryCodes,
} from "@hdms/api-client";
import { queryOptions } from "@tanstack/react-query";
import { queryClient } from "./query-client";

export const currentAdminQueryKey = ["auth", "me"] as const;

export const currentAdminQueryOptions = queryOptions({
  queryKey: currentAdminQueryKey,
  queryFn: async () => {
    const { data, error } = await getCurrentAdmin();
    if (error) throw error;
    return data;
  },
  staleTime: 5 * 60 * 1000,
  retry: false,
});

export interface LoginParams {
  email: string;
  password: string;
  totpCode?: string;
  recoveryCode?: string;
}

export async function loginAdmin(params: LoginParams) {
  const { data, error } = await login({ body: params });
  if (error) throw error;
  queryClient.setQueryData(currentAdminQueryKey, data);
  return data;
}

export async function logoutAdmin() {
  await logout();
  queryClient.setQueryData(currentAdminQueryKey, undefined);
  queryClient.removeQueries({ queryKey: currentAdminQueryKey });
}

export async function changeOwnPasswordAdmin(params: {
  currentPassword: string;
  newPassword: string;
}) {
  const { data, error } = await changeOwnPassword({ body: params });
  if (error) throw error;
  await queryClient.invalidateQueries({ queryKey: currentAdminQueryKey });
  return data;
}

export async function beginTotpReenrolmentAdmin() {
  const { data, error } = await beginTotpReenrolment();
  if (error) throw error;
  return data;
}

export async function confirmTotpReenrolmentAdmin(params: { totpCode: string }) {
  const { data, error } = await confirmTotpReenrolment({ body: params });
  if (error) throw error;
  await queryClient.invalidateQueries({ queryKey: currentAdminQueryKey });
  return data;
}

export async function regenerateRecoveryCodesAdmin() {
  const { data, error } = await regenerateRecoveryCodes();
  if (error) throw error;
  return data;
}

type SessionExpiredListener = () => void;
const sessionExpiredListeners = new Set<SessionExpiredListener>();

export function onSessionExpired(listener: SessionExpiredListener): () => void {
  sessionExpiredListeners.add(listener);
  return () => {
    sessionExpiredListeners.delete(listener);
  };
}

export function triggerSessionExpired() {
  for (const listener of sessionExpiredListeners) {
    try {
      listener();
    } catch {
      // Ignore listener errors
    }
  }
}

const CSRF_COOKIE = "hdms_csrf";
const MUTATING_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

function readCookie(name: string): string | undefined {
  const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
  return match ? decodeURIComponent(match[1]) : undefined;
}

// Wires the generated client for the admin's session-cookie + CSRF
// double-submit auth (docs/06-api-contract.md): cookies must flow across
// the dev-mode origin split (api on :8443, admin on :5174), and every
// mutating request needs the CSRF cookie echoed back as a header.
export function installAuthInterceptors() {
  client.setConfig({ credentials: "include" });

  client.interceptors.request.use((request) => {
    if (MUTATING_METHODS.has(request.method.toUpperCase())) {
      const token = readCookie(CSRF_COOKIE);
      if (token) request.headers.set("X-CSRF-Token", token);
    }
    return request;
  });

  client.interceptors.response.use((response, request) => {
    const isAuthCall =
      request.url.includes("/auth/login") ||
      request.url.includes("/auth/logout") ||
      request.url.includes("/auth/me");

    if (response.status === 401 && !isAuthCall) {
      queryClient.removeQueries({ queryKey: currentAdminQueryKey });
      // Base-aware: production serves under /admin (5.3a), dev at /.
      const base = (import.meta as any).env?.VITE_BASE_PATH ?? "/";
      const loginPath = base === "/" ? "/login" : `${base.replace(/\/$/, "")}/login`;
      if (sessionExpiredListeners.size > 0) {
        triggerSessionExpired();
      } else if (!window.location.pathname.startsWith(loginPath)) {
        window.location.href = loginPath;
      }
    }
    return response;
  });
}


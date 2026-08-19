import { client, getCurrentAdmin, login, logout } from "@hdms/api-client";
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

export async function loginAdmin(params: { email: string; password: string; totpCode: string }) {
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
    const isAuthCall = request.url.includes("/auth/login") || request.url.includes("/auth/me");
    if (response.status === 401 && !isAuthCall) {
      queryClient.removeQueries({ queryKey: currentAdminQueryKey });
      if (!window.location.pathname.startsWith("/login")) {
        window.location.href = "/login";
      }
    }
    return response;
  });
}

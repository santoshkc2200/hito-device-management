import {
  client,
  completeStaffProfile,
  getStaffMe,
  staffLogout,
  staffPasswordLogin,
  type StaffMe,
} from "@hdms/api-client";
import { queryOptions } from "@tanstack/react-query";
import { queryClient } from "./query-client";

export const currentStaffQueryKey = ["staff", "me"] as const;

export const currentStaffQueryOptions = queryOptions({
  queryKey: currentStaffQueryKey,
  queryFn: async () => {
    const { data, error } = await getStaffMe();
    if (error) throw error;
    return data;
  },
  staleTime: 5 * 60 * 1000,
  retry: false,
});

export async function loginWithPassword(input: {
  employeeNo: string;
  password: string;
}): Promise<StaffMe> {
  const { data, error } = await staffPasswordLogin({ body: input });
  if (error) throw error;
  queryClient.setQueryData(currentStaffQueryKey, data);
  return data;
}

export async function fetchMe(): Promise<StaffMe | null> {
  const { data, error } = await getStaffMe();
  if (error) return null;
  return data ?? null;
}

export function microsoftSignInUrl(): string {
  return "/v1/staff/auth/microsoft/start";
}

export async function logout(): Promise<void> {
  await staffLogout();
  queryClient.setQueryData(currentStaffQueryKey, null);
  queryClient.removeQueries({ queryKey: currentStaffQueryKey });
}

export async function completeProfile(input: {
  employeeNo: string;
  departmentId?: string;
}): Promise<StaffMe> {
  const { data, error } = await completeStaffProfile({ body: input });
  if (error) throw error;
  queryClient.setQueryData(currentStaffQueryKey, data);
  return data;
}

const CSRF_COOKIE = "hdms_staff_csrf";
const MUTATING_METHODS = new Set(["POST", "PUT", "PATCH", "DELETE"]);

function readCookie(name: string): string | undefined {
  if (typeof document === "undefined") return undefined;
  const match = document.cookie.match(new RegExp(`(?:^|; )${name}=([^;]*)`));
  return match ? decodeURIComponent(match[1]) : undefined;
}

let authInterceptorsInstalled = false;

export function installAuthInterceptors() {
  if (authInterceptorsInstalled) return;
  authInterceptorsInstalled = true;

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
      request.url.includes("/staff/auth/password") ||
      request.url.includes("/staff/auth/logout") ||
      request.url.includes("/staff/me");

    if (response.status === 401 && !isAuthCall) {
      queryClient.removeQueries({ queryKey: currentStaffQueryKey });
      // Base-aware: production serves under /staff (5.3a), dev at /.
      const base = (import.meta as any).env?.VITE_BASE_PATH ?? "/";
      const loginPath = base === "/" ? "/login" : `${base.replace(/\/$/, "")}/login`;
      if (
        typeof window !== "undefined" &&
        !window.location.pathname.startsWith(loginPath)
      ) {
        window.location.href = loginPath;
      }
    }
    return response;
  });
}

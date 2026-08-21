import { useQuery } from "@tanstack/react-query";
import type { AdminRole } from "@hdms/api-client";
import { currentAdminQueryOptions } from "./auth";
import type { ReactNode } from "react";

export function getRoleRank(role?: AdminRole | string): number {
  switch (role) {
    case "superadmin":
    case "admin":
      return 3;
    case "operator":
    case "technician":
      return 2;
    case "viewer":
      return 1;
    default:
      return 0;
  }
}

export function hasRoleAtLeast(
  userRole?: AdminRole | string,
  minRole?: "viewer" | "technician" | "admin"
): boolean {
  if (!minRole) return true;
  const userRank = getRoleRank(userRole);
  const requiredRank = getRoleRank(minRole);
  return userRank >= requiredRank;
}

export function useRole() {
  const { data: admin, isLoading } = useQuery(currentAdminQueryOptions);
  const role = admin?.role;

  const isAdmin = role === "admin" || role === "superadmin";
  const isTechnician = role === "technician" || role === "operator";
  const isViewer = role === "viewer";

  return {
    admin,
    role,
    isLoading,
    isAdmin,
    isTechnician,
    isViewer,
    hasMinRole: (minRole: "viewer" | "technician" | "admin") =>
      hasRoleAtLeast(role, minRole),
    canManageAdmins: isAdmin,
    canManageSettings: isAdmin,
    canMutateDevices: isAdmin || isTechnician,
    canMutateUsers: isAdmin || isTechnician,
    canPerformBackfill: isAdmin || isTechnician,
    canManageCredentials: isAdmin || isTechnician,
  };
}

export interface RoleGateProps {
  minRole: "viewer" | "technician" | "admin";
  fallback?: ReactNode;
  children: ReactNode;
}

export function RoleGate({ minRole, fallback = null, children }: RoleGateProps) {
  const { hasMinRole, isLoading } = useRole();

  if (isLoading) return null;
  if (!hasMinRole(minRole)) return <>{fallback}</>;

  return <>{children}</>;
}
